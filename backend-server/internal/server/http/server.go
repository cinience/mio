package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	log "backend-server/internal/infrastructure/logger"
)

// Option configures the HTTP server during construction.
type Option func(*Server)

// WithFallbackProxyURL configures the reverse proxy target for unmatched routes.
func WithFallbackProxyURL(rawURL string) Option {
	return func(s *Server) {
		s.fallbackURL = rawURL
	}
}

// WithTimeouts configures custom read/write/idle timeouts.
func WithTimeouts(read, write, idle time.Duration) Option {
	return func(s *Server) {
		if read > 0 {
			s.readTimeout = read
		}
		if write > 0 {
			s.writeTimeout = write
		}
		if idle > 0 {
			s.idleTimeout = idle
		}
	}
}

// Server wraps a Gin engine and underlying http.Server instance.
type Server struct {
	engine       *gin.Engine
	httpServer   *nethttp.Server
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	extraServers []*nethttp.Server

	fallbackURL    string
	fallbackProxy  *httputil.ReverseProxy
	fallbackTarget *url.URL
}

// NewServer constructs a new HTTP server bound to listenAddr.
func NewServer(listenAddr string, opts ...Option) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		engine:       gin.New(),
		readTimeout:  15 * time.Second,
		writeTimeout: 15 * time.Second,
		idleTimeout:  60 * time.Second,
	}
	s.engine.Use(gin.Recovery())

	for _, opt := range opts {
		opt(s)
	}

	if err := s.configureFallback(); err != nil {
		return nil, err
	}

	s.httpServer = &nethttp.Server{
		Addr:         listenAddr,
		Handler:      s.engine,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
		IdleTimeout:  s.idleTimeout,
	}

	return s, nil
}

// Engine exposes the underlying Gin engine for route registration.
func (s *Server) Engine() *gin.Engine {
	if s == nil {
		return nil
	}
	return s.engine
}

// FallbackTarget returns the configured proxy target, if any.
func (s *Server) FallbackTarget() *url.URL {
	if s == nil {
		return nil
	}
	return s.fallbackTarget
}

// Start begins serving HTTP requests until the server is shutdown.
func (s *Server) Start() error {
	if s == nil || s.httpServer == nil {
		return fmt.Errorf("http server not initialized")
	}
	s.tryListenOnPort80()
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var firstErr error

	for _, extra := range s.extraServers {
		if extra == nil {
			continue
		}
		if err := extra.Shutdown(ctx); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			if firstErr == nil {
				firstErr = err
			} else {
				log.Warnf("error shutting down auxiliary http server: %v", err)
			}
		}
	}

	if s.httpServer == nil {
		return firstErr
	}

	if err := s.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
		if firstErr == nil {
			return err
		}
		log.Warnf("error shutting down primary http server: %v", err)
	}
	return firstErr
}

func (s *Server) configureFallback() error {
	if s.fallbackURL == "" {
		return nil
	}

	target, err := url.Parse(s.fallbackURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return fmt.Errorf("invalid fallback proxy url: %s", s.fallbackURL)
	}

	s.fallbackTarget = target
	s.fallbackProxy = httputil.NewSingleHostReverseProxy(target)

	if s.engine != nil {
		s.engine.NoRoute(s.forwardToFallback)
		s.engine.NoMethod(s.forwardToFallback)
	}

	log.Infof("HTTP 未匹配请求将代理到: %s", target.String())
	return nil
}

func (s *Server) forwardToFallback(c *gin.Context) {
	if s == nil || s.fallbackProxy == nil {
		c.AbortWithStatus(nethttp.StatusNotFound)
		return
	}

	req := c.Request
	if isWebSocketRequest(req) {
		if err := s.forwardWebSocket(c.Writer, req); err != nil {
			log.Warnf("WebSocket fallback proxy failed: %v", err)
			if !c.Writer.Written() {
				c.AbortWithStatus(nethttp.StatusBadGateway)
				return
			}
		}
		c.Abort()
		return
	}

	//log.Debugf("代理未匹配请求 %s %s 到 %s", c.Request.Method, c.Request.URL.String(), s.fallbackTarget.String())
	defer func() {
		if rec := recover(); rec != nil {
			log.Warnf("fallback proxy panic suppressed, target=%s, err=%v", s.fallbackTarget, rec)
			if !c.Writer.Written() {
				c.AbortWithStatus(nethttp.StatusBadGateway)
			}
		}
	}()
	s.fallbackProxy.ServeHTTP(c.Writer, req)
	c.Abort()
}

func (s *Server) tryListenOnPort80() {
	if s == nil || s.httpServer == nil {
		return
	}

	host, port, err := net.SplitHostPort(s.httpServer.Addr)
	if err != nil {
		log.Warnf("无法解析HTTP监听地址 %q: %v", s.httpServer.Addr, err)
		return
	}

	if port == "80" {
		return
	}

	addr80 := net.JoinHostPort(host, "80")
	listener, err := net.Listen("tcp", addr80)
	if err != nil {
		log.Warnf("尝试监听80端口失败，将继续使用 %s: %v", s.httpServer.Addr, err)
		return
	}

	server := &nethttp.Server{
		Addr:         addr80,
		Handler:      s.engine,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
		IdleTimeout:  s.idleTimeout,
	}
	s.extraServers = append(s.extraServers, server)

	log.Infof("HTTP 服务器额外监听端口: %s", addr80)

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			log.Warnf("80端口监听异常退出: %v", err)
		}
	}()
}

func (s *Server) forwardWebSocket(w nethttp.ResponseWriter, r *nethttp.Request) error {
	if s == nil {
		return errors.New("server is nil")
	}
	targetURL, err := s.websocketTargetForRequest(r)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		Proxy:            nethttp.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}
	if extensions := strings.Join(r.Header.Values("Sec-WebSocket-Extensions"), ","); strings.Contains(strings.ToLower(extensions), "permessage-deflate") {
		dialer.EnableCompression = true
	}
	dialer.Subprotocols = parseWebSocketSubprotocols(r.Header)

	requestHeader := prepareWebSocketRequestHeader(r)
	backendConn, resp, err := dialer.DialContext(r.Context(), targetURL.String(), requestHeader)
	if err != nil {
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		return fmt.Errorf("dial websocket backend %q failed: %w", targetURL.String(), err)
	}
	defer backendConn.Close()

	selectedProto := backendConn.Subprotocol()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*nethttp.Request) bool { return true },
		Subprotocols: func() []string {
			if selectedProto == "" {
				return nil
			}
			return []string{selectedProto}
		}(),
	}

	responseHeader := nethttp.Header{}
	if resp != nil {
		if v := resp.Header.Get("Sec-WebSocket-Extensions"); v != "" {
			responseHeader.Set("Sec-WebSocket-Extensions", v)
			if strings.Contains(strings.ToLower(v), "permessage-deflate") {
				upgrader.EnableCompression = true
			}
		}
	}

	clientConn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return fmt.Errorf("upgrade client connection failed: %w", err)
	}
	defer clientConn.Close()

	if upgrader.EnableCompression {
		clientConn.EnableWriteCompression(true)
		backendConn.EnableWriteCompression(true)
	}

	errc := make(chan error, 2)
	go proxyWebSocketConn(backendConn, clientConn, errc)
	go proxyWebSocketConn(clientConn, backendConn, errc)

	if proxyErr := <-errc; !isExpectedWebSocketError(proxyErr) {
		return proxyErr
	}
	return nil
}

func (s *Server) websocketTargetForRequest(r *nethttp.Request) (*url.URL, error) {
	if s.fallbackTarget == nil {
		return nil, errors.New("fallback target not configured")
	}
	target := *s.fallbackTarget
	target.Path = joinProxyPath(target.Path, r.URL.Path)
	target.RawQuery = r.URL.RawQuery
	target.Fragment = ""
	target.Scheme = websocketSchemeFor(target.Scheme, r)
	return &target, nil
}

func websocketSchemeFor(scheme string, r *nethttp.Request) string {
	switch strings.ToLower(scheme) {
	case "ws", "wss":
		return strings.ToLower(scheme)
	case "https":
		return "wss"
	case "http":
		return "ws"
	default:
		if r != nil && r.TLS != nil {
			return "wss"
		}
		return "ws"
	}
}

func parseWebSocketSubprotocols(header nethttp.Header) []string {
	var protocols []string
	values := header.Values("Sec-WebSocket-Protocol")
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				protocols = append(protocols, trimmed)
			}
		}
	}
	return protocols
}

func prepareWebSocketRequestHeader(r *nethttp.Request) nethttp.Header {
	header := nethttp.Header{}
	for key, values := range r.Header {
		lower := strings.ToLower(key)
		switch lower {
		case "connection",
			"upgrade",
			"sec-websocket-key",
			"sec-websocket-version",
			"sec-websocket-protocol",
			"sec-websocket-extensions":
			continue
		}
		for _, value := range values {
			header.Add(key, value)
		}
	}

	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
			header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			header.Set("X-Forwarded-For", clientIP)
		}
	}

	return header
}

func proxyWebSocketConn(dst, src *websocket.Conn, errc chan<- error) {
	for {
		msgType, reader, err := src.NextReader()
		if err != nil {
			errc <- err
			return
		}
		writer, err := dst.NextWriter(msgType)
		if err != nil {
			errc <- err
			return
		}
		if _, err := io.Copy(writer, reader); err != nil {
			_ = writer.Close()
			errc <- err
			return
		}
		if err := writer.Close(); err != nil {
			errc <- err
			return
		}
	}
}

func isExpectedWebSocketError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	if ce, ok := err.(*websocket.CloseError); ok {
		switch ce.Code {
		case websocket.CloseNormalClosure, websocket.CloseNoStatusReceived, websocket.CloseGoingAway:
			return true
		}
	}
	return false
}

func isWebSocketRequest(r *nethttp.Request) bool {
	if r == nil {
		return false
	}
	connHeader := strings.ToLower(r.Header.Get("Connection"))
	upgradeHeader := strings.ToLower(r.Header.Get("Upgrade"))
	return strings.Contains(connHeader, "upgrade") && upgradeHeader == "websocket"
}

func joinProxyPath(a, b string) string {
	switch {
	case strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/"):
		return a + b[1:]
	case !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/"):
		if a == "" {
			return "/" + b
		}
		return a + "/" + b
	default:
		return a + b
	}
}
