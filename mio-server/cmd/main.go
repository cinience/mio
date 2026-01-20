package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	appconfig "mio-server/internal/config"
	apphttp "mio-server/internal/http"
	"mio-server/internal/ws"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := appconfig.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	wsHandler := ws.NewHandler(logger, cfg)
	router := apphttp.NewRouter(cfg, wsHandler, logger)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		if err := listen(server, cfg, logger); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server error", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown failed", zap.Error(err))
	}
}

func listen(server *http.Server, cfg appconfig.Config, logger *zap.Logger) error {
	if cfg.TLSDisable {
		logger.Info("starting http server", zap.String("addr", cfg.HTTPAddr))
		return server.ListenAndServe()
	}

	certPath := filepath.Clean(cfg.TLSCertPath)
	keyPath := filepath.Clean(cfg.TLSKeyPath)
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	if certExists && keyExists {
		logger.Info("starting https server", zap.String("addr", cfg.HTTPAddr))
		return server.ListenAndServeTLS(certPath, keyPath)
	}

	if cfg.TLSRequired {
		missing := []string{}
		if !certExists {
			missing = append(missing, certPath)
		}
		if !keyExists {
			missing = append(missing, keyPath)
		}
		logger.Warn("tls required but certs missing; using in-memory cert", zap.Strings("missing", missing))
	}

	cert, err := generateSelfSignedCert(cfg.SystemConfig.Host)
	if err != nil {
		return fmt.Errorf("failed to generate tls cert: %w", err)
	}
	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	logger.Info("starting https server with in-memory cert", zap.String("addr", cfg.HTTPAddr))
	return server.ListenAndServeTLS("", "")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func generateSelfSignedCert(host string) (tls.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	notBefore := time.Now().Add(-time.Minute)
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	dnsNames := []string{"localhost"}
	ipAddresses := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}

	if host != "" && host != "0.0.0.0" && host != "::" {
		if ip := net.ParseIP(host); ip != nil {
			ipAddresses = appendIP(ipAddresses, ip)
		} else {
			dnsNames = append(dnsNames, host)
		}
	}

	ifaces, _ := net.InterfaceAddrs()
	for _, addr := range ifaces {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		ipAddresses = appendIP(ipAddresses, ip)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkixName("mio-local"),
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     uniqueStrings(dnsNames),
		IPAddresses:  uniqueIPs(ipAddresses),
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return tls.X509KeyPair(certPEM, keyPEM)
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{
		CommonName:   commonName,
		Organization: []string{"mio"},
	}
}

func appendIP(list []net.IP, ip net.IP) []net.IP {
	for _, existing := range list {
		if existing.Equal(ip) {
			return list
		}
	}
	return append(list, ip)
}

func uniqueIPs(list []net.IP) []net.IP {
	unique := make([]net.IP, 0, len(list))
	for _, ip := range list {
		if ip == nil {
			continue
		}
		unique = appendIP(unique, ip)
	}
	return unique
}

func uniqueStrings(list []string) []string {
	unique := make([]string, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		unique = append(unique, item)
	}
	return unique
}
