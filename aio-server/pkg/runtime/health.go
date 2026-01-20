package runtime

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

const (
	defaultBackendHealthURL = "http://127.0.0.1:8002/health"
	defaultManagerHealthURL = "http://127.0.0.1:8007/xiaozhi/health"
	defaultSpeechHealthURL  = "http://127.0.0.1:8009/health"
)

func (r *Runtime) backendHealthCheck(ctx context.Context) error {
	endpoint, err := r.backendHealthEndpoint()
	if err != nil {
		log.Printf("[aio-supervisor] backend health endpoint fallback: %v", err)
	}
	return httpHealthCheck(ctx, endpoint)
}

func (r *Runtime) managerHealthCheck(ctx context.Context) error {
	endpoint, err := r.managerHealthEndpoint()
	if err != nil {
		log.Printf("[aio-supervisor] manager health endpoint fallback: %v", err)
	}
	return httpHealthCheck(ctx, endpoint)
}

func (r *Runtime) speechHealthCheck(ctx context.Context) error {
	endpoint, err := r.speechHealthEndpoint()
	if err != nil {
		log.Printf("[aio-supervisor] speech health endpoint fallback: %v", err)
	}
	return httpHealthCheck(ctx, endpoint)
}

func (r *Runtime) redisHealthCheck(ctx context.Context) error {
	r.mu.Lock()
	addr := strings.TrimSpace(r.redisAddress)
	password := r.redisPassword
	r.mu.Unlock()

	if addr == "" {
		return fmt.Errorf("redis address not configured")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	defer client.Close()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

func (r *Runtime) backendHealthEndpoint() (string, error) {
	r.backendEndpointOnce.Do(func() {
		endpoint, err := deriveBackendHealthEndpoint(r.cfg.BackendConfig)
		if err != nil {
			log.Printf("[aio-supervisor] derive backend health endpoint: %v", err)
		}
		if endpoint == "" {
			endpoint = defaultBackendHealthURL
		}
		r.backendEndpoint = endpoint
		r.backendEndpointErr = err
	})
	return r.backendEndpoint, r.backendEndpointErr
}

func (r *Runtime) managerHealthEndpoint() (string, error) {
	r.managerEndpointOnce.Do(func() {
		endpoint, err := deriveManagerHealthEndpoint(r.cfg.ManagerConfig)
		if err != nil {
			log.Printf("[aio-supervisor] derive manager health endpoint: %v", err)
		}
		if endpoint == "" {
			endpoint = defaultManagerHealthURL
		}
		r.managerEndpoint = endpoint
		r.managerEndpointErr = err
	})
	return r.managerEndpoint, r.managerEndpointErr
}

func (r *Runtime) speechHealthEndpoint() (string, error) {
	r.speechEndpointOnce.Do(func() {
		endpoint, err := deriveSpeechHealthEndpoint(r.cfg.SpeechConfig)
		if err != nil {
			log.Printf("[aio-supervisor] derive speech health endpoint: %v", err)
		}
		if endpoint == "" {
			endpoint = defaultSpeechHealthURL
		}
		r.speechEndpoint = endpoint
		r.speechEndpointErr = err
	})
	return r.speechEndpoint, r.speechEndpointErr
}

func deriveBackendHealthEndpoint(configPath string) (string, error) {
	host := "127.0.0.1"
	port := 8002

	path := strings.TrimSpace(configPath)
	if path == "" {
		return fmt.Sprintf("http://%s:%d/health", host, port), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("http://%s:%d/health", host, port), err
	}

	var cfg struct {
		WebSocket struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"websocket"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Sprintf("http://%s:%d/health", host, port), err
	}

	if h := strings.TrimSpace(cfg.WebSocket.Host); h != "" && h != "0.0.0.0" && h != "::" {
		host = h
	}
	if cfg.WebSocket.Port > 0 {
		port = cfg.WebSocket.Port
	}

	return fmt.Sprintf("http://%s:%d/health", host, port), nil
}

func deriveManagerHealthEndpoint(configPath string) (string, error) {
	host := "127.0.0.1"
	port := 8007

	path := strings.TrimSpace(configPath)
	if path == "" {
		return fmt.Sprintf("http://%s:%d/xiaozhi/health", host, port), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("http://%s:%d/xiaozhi/health", host, port), err
	}

	var cfg struct {
		Server struct {
			Port int    `yaml:"port"`
			Host string `yaml:"host"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Sprintf("http://%s:%d/xiaozhi/health", host, port), err
	}

	if h := strings.TrimSpace(cfg.Server.Host); h != "" && h != "0.0.0.0" && h != "::" {
		host = h
	}
	if cfg.Server.Port > 0 {
		port = cfg.Server.Port
	}

	return fmt.Sprintf("http://%s:%d/xiaozhi/health", host, port), nil
}

func deriveSpeechHealthEndpoint(configPath string) (string, error) {
	addr := ":8009"

	path := strings.TrimSpace(configPath)
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("http://%s/health", normalizeSpeechAddr(addr)), err
		}

		var cfg struct {
			Server struct {
				Addr string `yaml:"addr"`
			} `yaml:"server"`
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Sprintf("http://%s/health", normalizeSpeechAddr(addr)), err
		}
		if strings.TrimSpace(cfg.Server.Addr) != "" {
			addr = cfg.Server.Addr
		}
	}

	return fmt.Sprintf("http://%s/health", normalizeSpeechAddr(addr)), nil
}

func normalizeSpeechAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:8009"
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "[::]:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "[::]:")
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}

func httpHealthCheck(ctx context.Context, endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("empty health endpoint")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
