package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	redisservice "aio-server/internal/redisservice"
	backend "backend-server/pkg/runtime"
	manager "manager-server/pkg/runtime"

	"gopkg.in/yaml.v3"
)

const (
	ServiceBackend = "backend"
	ServiceManager = "manager"
	ServiceRedis   = "redis"
	ServiceSpeech  = "speech"

	managerReadyTimeout         = 30 * time.Second
	defaultManagerHealthTimeout = 5 * time.Second
	defaultHealthCheckInterval  = 30 * time.Second
	defaultHealthCheckTimeout   = 5 * time.Second
	defaultRestartBackoff       = 15 * time.Second
)

// Config defines the inputs required to bootstrap the bundled services.
type Config struct {
	Version string

	BackendConfig string
	ManagerConfig string
	SpeechConfig  string

	Redis redisservice.Config

	// RedisAddress is used to configure the backend when Redis runs elsewhere.
	RedisAddress string

	// RedisPassword overrides the backend redis password; when empty and
	// DeriveRedisPassword is true, it is derived from the backend config.
	RedisPassword        string
	DeriveRedisPassword  bool
	OnManagerRunComplete func(error)

	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
	RestartBackoff      time.Duration
}

// Status represents coarse service state.
type Status struct {
	Backend bool
	Manager bool
	Redis   bool
	Speech  bool
}

type backendService interface {
	Start()
	Shutdown(context.Context) error
}

type managerService interface {
	Run() error
	Shutdown(context.Context) error
	Addr() string
}

type speechService interface {
	Start() error
	Shutdown(context.Context) error
	Addr() string
}

// Runtime coordinates the lifecycle of backend, manager, and redis services.
type Runtime struct {
	cfg Config

	mu sync.Mutex

	backend     backendService
	manager     managerService
	managerDone chan error

	redis       *redisservice.Service
	redisActive bool

	redisAddress  string
	redisPassword string

	supervisorCancel context.CancelFunc
	supervisorDone   chan struct{}

	monitorCancel context.CancelFunc
	monitorDone   chan struct{}

	lastRestart map[string]time.Time

	backendEndpointOnce sync.Once
	backendEndpoint     string
	backendEndpointErr  error

	managerEndpointOnce sync.Once
	managerEndpoint     string
	managerEndpointErr  error

	speech speechService

	speechEndpointOnce sync.Once
	speechEndpoint     string
	speechEndpointErr  error
}

// New returns a Runtime configured with the provided options.
func New(cfg Config) (*Runtime, error) {
	cfg.Version = strings.TrimSpace(cfg.Version)
	if cfg.Version == "" {
		cfg.Version = "unknown"
	}

	cfg.BackendConfig = strings.TrimSpace(cfg.BackendConfig)
	cfg.ManagerConfig = strings.TrimSpace(cfg.ManagerConfig)
	cfg.SpeechConfig = strings.TrimSpace(cfg.SpeechConfig)
	cfg.RedisAddress = strings.TrimSpace(cfg.RedisAddress)
	cfg.RedisPassword = strings.TrimSpace(cfg.RedisPassword)

	if cfg.RedisAddress == "" {
		cfg.RedisAddress = strings.TrimSpace(cfg.Redis.Address)
	}
	if cfg.RedisAddress == "" {
		cfg.RedisAddress = "127.0.0.1:26379"
	}

	if cfg.RedisPassword == "" && cfg.DeriveRedisPassword {
		cfg.RedisPassword = deriveRedisPassword(cfg.BackendConfig)
	}

	if cfg.HealthCheckInterval < 0 {
		cfg.HealthCheckInterval = 0
	} else if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = defaultHealthCheckInterval
	}
	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = defaultHealthCheckTimeout
	}
	if cfg.RestartBackoff <= 0 {
		cfg.RestartBackoff = defaultRestartBackoff
	}

	rt := &Runtime{
		cfg:           cfg,
		redisAddress:  cfg.RedisAddress,
		redisPassword: cfg.RedisPassword,
		lastRestart:   make(map[string]time.Time),
	}

	return rt, nil
}

// Start launches the selected services. When no services are specified, all are started.
func (r *Runtime) Start(ctx context.Context, services ...string) error {
	selection, err := resolveServices(services)
	if err != nil {
		return err
	}

	var started []string
	rollback := func() {
		if len(started) == 0 {
			return
		}
		names := make([]string, len(started))
		copy(names, started)
		// stop in reverse order
		for i := len(names)/2 - 1; i >= 0; i-- {
			opp := len(names) - 1 - i
			names[i], names[opp] = names[opp], names[i]
		}
		_ = r.Stop(ctx, names...)
	}

	if selection[ServiceRedis] {
		if err := r.startRedis(ctx); err != nil {
			return err
		}
		started = append(started, ServiceRedis)
	}

	if selection[ServiceManager] {
		if err := r.startManager(ctx); err != nil {
			rollback()
			return err
		}
		started = append(started, ServiceManager)

		if selection[ServiceBackend] {
			if err := r.waitForManagerReady(ctx); err != nil {
				rollback()
				return err
			}
		}
	}

	if selection[ServiceBackend] {
		if err := r.configureBackendRedis(); err != nil {
			rollback()
			return err
		}
		if err := r.startBackend(ctx); err != nil {
			rollback()
			return err
		}
		started = append(started, ServiceBackend)
	}

	if selection[ServiceSpeech] {
		if err := r.startSpeech(ctx); err != nil {
			rollback()
			return err
		}
		started = append(started, ServiceSpeech)
	}

	r.startSupervisor()
	r.startResourceMonitor()

	return nil
}

// Stop stops the selected services. When no services are specified, all are stopped.
func (r *Runtime) Stop(ctx context.Context, services ...string) error {
	selection, err := resolveServices(services)
	if err != nil {
		return err
	}

	var errs []error
	appendErr := func(name string, err error) {
		if err == nil {
			return
		}
		errs = append(errs, fmt.Errorf("%s: %w", name, err))
	}

	if selection[ServiceManager] {
		appendErr(ServiceManager, r.stopManager(ctx))
	}
	if selection[ServiceSpeech] {
		appendErr(ServiceSpeech, r.stopSpeech(ctx))
	}
	if selection[ServiceBackend] {
		appendErr(ServiceBackend, r.stopBackend(ctx))
	}
	if selection[ServiceRedis] {
		appendErr(ServiceRedis, r.stopRedis(ctx))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Shutdown stops all managed services.
func (r *Runtime) Shutdown(ctx context.Context) error {
	r.stopSupervisor()
	r.stopResourceMonitor()
	return r.Stop(ctx, availableServices...)
}

// Status returns the current running state of each service.
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	return Status{
		Backend: r.backend != nil,
		Manager: r.manager != nil,
		Redis:   r.redisActive && r.redis != nil,
		Speech:  r.speech != nil,
	}
}

// ManagerErrors exposes the manager run result channel, if available.
func (r *Runtime) ManagerErrors() <-chan error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.managerDone
}

// RedisAddr reports the effective redis endpoint used by the runtime.
func (r *Runtime) RedisAddr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.redisAddress
}

func (r *Runtime) startRedis(ctx context.Context) error {
	r.mu.Lock()
	if r.redisActive && r.redis != nil {
		r.mu.Unlock()
		return nil
	}
	cfg := r.cfg.Redis
	cfg.Address = strings.TrimSpace(cfg.Address)
	if cfg.Address == "" {
		cfg.Address = r.redisAddress
	}
	cfg.Password = r.redisPassword
	r.mu.Unlock()

	var service *redisservice.Service
	if err := protectServiceCall("redis.New", func() error {
		var err error
		service, err = redisservice.New(cfg)
		return err
	}); err != nil {
		return err
	}
	if err := protectServiceCall("redis.Start", service.Start); err != nil {
		return err
	}

	r.mu.Lock()
	r.redis = service
	r.redisActive = true
	r.redisAddress = service.Address()
	r.mu.Unlock()

	return nil
}

func (r *Runtime) startBackend(ctx context.Context) error {
	r.mu.Lock()
	if r.backend != nil {
		r.mu.Unlock()
		return nil
	}
	version := r.cfg.Version
	configPath := r.cfg.BackendConfig
	r.mu.Unlock()

	if configPath == "" {
		return fmt.Errorf("backend config path is required")
	}

	var instance backendService
	if err := protectServiceCall("backend.New", func() error {
		server, err := backend.New(version, configPath)
		if err != nil {
			return err
		}
		instance = server
		return nil
	}); err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("backend service initialization returned nil instance")
	}
	if err := protectServiceCall("backend.Start", func() error {
		instance.Start()
		return nil
	}); err != nil {
		return err
	}

	r.mu.Lock()
	r.backend = instance
	r.mu.Unlock()

	return nil
}

func (r *Runtime) startManager(ctx context.Context) error {
	r.mu.Lock()
	if r.manager != nil {
		r.mu.Unlock()
		return nil
	}
	configPath := r.cfg.ManagerConfig
	r.mu.Unlock()

	if configPath == "" {
		return fmt.Errorf("manager config path is required")
	}

	var instance managerService
	if err := protectServiceCall("manager.New", func() error {
		server, err := manager.New(configPath)
		if err != nil {
			return err
		}
		instance = server
		return nil
	}); err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("manager service initialization returned nil instance")
	}

	done := make(chan error, 1)
	r.mu.Lock()
	r.manager = instance
	r.managerDone = done
	onComplete := r.cfg.OnManagerRunComplete
	r.mu.Unlock()

	go func(s managerService, ch chan<- error, callback func(error)) {
		err := protectServiceCall("manager.Run", s.Run)
		if err != nil && callback != nil {
			_ = protectServiceCall("manager.OnManagerRunComplete", func() error {
				callback(err)
				return nil
			})
		}
		select {
		case ch <- err:
		default:
		}
	}(instance, done, onComplete)

	return nil
}

func (r *Runtime) waitForManagerReady(ctx context.Context) error {
	r.mu.Lock()
	instance := r.manager
	addr := ""
	if instance != nil {
		addr = strings.TrimSpace(instance.Addr())
	}
	attemptTimeout := r.cfg.HealthCheckTimeout
	r.mu.Unlock()

	if instance == nil {
		return errors.New("manager service not initialized")
	}

	if attemptTimeout <= 0 {
		attemptTimeout = defaultManagerHealthTimeout
	}

	target := normalizeManagerAddr(addr)
	if target == "" {
		return fmt.Errorf("invalid manager address: %q", addr)
	}

	healthURL := fmt.Sprintf("http://%s/xiaozhi/health", target)
	deadline := time.Now().Add(managerReadyTimeout)
	client := &http.Client{}
	var lastErr error
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		reqCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, healthURL, nil)
		if err != nil {
			cancel()
			return err
		}

		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				cancel()
				return nil
			}
			err = fmt.Errorf("manager health returned %d", resp.StatusCode)
		}
		cancel()
		lastErr = err

		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("manager server not ready at %s within %s: %w", healthURL, managerReadyTimeout, lastErr)
			}
			return fmt.Errorf("manager server not ready at %s within %s", healthURL, managerReadyTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func normalizeManagerAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
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

func (r *Runtime) configureBackendRedis() error {
	r.mu.Lock()
	addr := strings.TrimSpace(r.redisAddress)
	password := r.redisPassword
	r.mu.Unlock()

	if addr == "" {
		return nil
	}
	return overrideBackendRedis(addr, password)
}

func (r *Runtime) stopManager(ctx context.Context) error {
	r.mu.Lock()
	instance := r.manager
	if instance == nil {
		r.mu.Unlock()
		return nil
	}
	done := r.managerDone
	r.manager = nil
	r.managerDone = nil
	r.mu.Unlock()

	err := instance.Shutdown(ctx)

	if done != nil {
		select {
		case <-ctx.Done():
		case <-done:
		}
	}

	return err
}

func (r *Runtime) stopBackend(ctx context.Context) error {
	r.mu.Lock()
	instance := r.backend
	if instance == nil {
		r.mu.Unlock()
		return nil
	}
	r.backend = nil
	r.mu.Unlock()

	err := instance.Shutdown(ctx)
	cleanupErr := backend.Cleanup()

	if err != nil && cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	}
	if err != nil {
		return err
	}
	return cleanupErr
}

func (r *Runtime) stopRedis(ctx context.Context) error {
	r.mu.Lock()
	service := r.redis
	active := r.redisActive
	if !active || service == nil {
		r.mu.Unlock()
		return nil
	}
	r.redisActive = false
	r.redis = nil
	r.mu.Unlock()

	return service.Shutdown(ctx)
}

func resolveServices(selected []string) (map[string]bool, error) {
	result := make(map[string]bool, len(availableServices))
	for _, svc := range availableServices {
		result[svc] = false
	}

	if len(selected) == 0 {
		for k := range result {
			result[k] = true
		}
		return result, nil
	}

	for _, svc := range selected {
		name := strings.ToLower(strings.TrimSpace(svc))
		if _, ok := result[name]; !ok {
			return nil, fmt.Errorf("unknown service %q (valid options: %s)", svc, strings.Join(availableServices, ", "))
		}
		result[name] = true
	}

	var hasAny bool
	for _, enabled := range result {
		if enabled {
			hasAny = true
			break
		}
	}

	if !hasAny {
		return nil, fmt.Errorf("no services selected (valid options: %s)", strings.Join(availableServices, ", "))
	}

	return result, nil
}

func overrideBackendRedis(addr, password string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("invalid redis address %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}

	set := func(key, value string) error {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
		return nil
	}

	if err := set("XIAOZHI_REDIS_HOST", host); err != nil {
		return err
	}
	if err := set("XIAOZHI_REDIS_PORT", port); err != nil {
		return err
	}
	if err := set("XIAOZHI_MEMORY_SHORT_TERM_REDIS_HOST", host); err != nil {
		return err
	}
	if err := set("XIAOZHI_MEMORY_SHORT_TERM_REDIS_PORT", port); err != nil {
		return err
	}
	if err := set("XIAOZHI_REDIS_PASSWORD", password); err != nil {
		return err
	}
	if err := set("XIAOZHI_MEMORY_SHORT_TERM_REDIS_PASSWORD", password); err != nil {
		return err
	}
	if err := set("XIAOZHI_WEBSOCKET_FALLBACK_PROXY_URL", "http://localhost:8007"); err != nil {
		return err
	}
	if err := set("XIAOZHI_MANAGER_API_BASE_URL", "http://localhost:8007/xiaozhi"); err != nil {
		return err
	}

	return nil
}

func deriveRedisPassword(configPath string) string {
	type root struct {
		Redis struct {
			Password string `yaml:"password"`
		} `yaml:"redis"`
	}

	path := strings.TrimSpace(configPath)
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var cfg root
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.Redis.Password
}
