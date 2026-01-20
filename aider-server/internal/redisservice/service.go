package redisservice

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/hdt3213/godis/aof"
	"github.com/hdt3213/godis/config"
	"github.com/hdt3213/godis/lib/logger"
	"github.com/hdt3213/godis/lib/utils"
	redisserver "github.com/hdt3213/godis/redis/server"
	"github.com/hdt3213/godis/tcp"
)

// Config captures the minimal settings required to run an embedded Redis server.
type Config struct {
	Address           string
	ConfigPath        string
	LogDir            string
	DataDir           string
	MaxClients        int
	Password          string
	AOFEnabled        *bool
	AOFFilename       string
	AOFFsync          string
	AOFUseRdbPreamble *bool
}

// Service manages lifecycle of the embedded Redis server.
type Service struct {
	cfg      Config
	listener net.Listener
	shutdown chan struct{}
	done     chan struct{}

	mu        sync.Mutex
	started   bool
	closeOnce sync.Once
}

// New constructs a Service instance with sane defaults.
func New(cfg Config) (*Service, error) {
	cfg.Address = strings.TrimSpace(cfg.Address)
	cfg.ConfigPath = strings.TrimSpace(cfg.ConfigPath)
	if cfg.Address == "" && cfg.ConfigPath == "" {
		cfg.Address = "127.0.0.1:26371"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join("logs", "redis")
	} else {
		cfg.LogDir = filepath.Clean(cfg.LogDir)
	}
	if cfg.DataDir != "" {
		cfg.DataDir = filepath.Clean(cfg.DataDir)
	}
	return &Service{cfg: cfg}, nil
}

// Start boots the Redis server and begins accepting connections.
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("redis service already started")
	}

	if err := s.prepare(); err != nil {
		return err
	}

	handler := redisserver.MakeHandler()
	listener, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("start redis listener at %s: %w", s.cfg.Address, err)
	}

	shutdown := make(chan struct{})
	done := make(chan struct{})

	s.listener = listener
	s.shutdown = shutdown
	s.done = done
	s.started = true
	s.closeOnce = sync.Once{}

	go func(l net.Listener, ch <-chan struct{}, finished chan<- struct{}) {
		tcp.ListenAndServe(l, handler, ch)
		close(finished)
	}(listener, shutdown, done)

	return nil
}

// Shutdown attempts to stop the Redis server within the provided context deadline.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	shutdown := s.shutdown
	done := s.done
	listener := s.listener
	s.mu.Unlock()

	s.closeOnce.Do(func() {
		if shutdown != nil {
			close(shutdown)
		}
		if listener != nil {
			_ = listener.Close()
		}
	})

	select {
	case <-done:
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Address returns the address the embedded Redis server is bound to.
func (s *Service) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Address
}

func (s *Service) prepare() error {
	if err := os.MkdirAll(s.cfg.LogDir, 0o755); err != nil {
		return fmt.Errorf("create redis log dir %q: %w", s.cfg.LogDir, err)
	}
	logger.Setup(&logger.Settings{
		Path:       s.cfg.LogDir,
		Name:       "redis-server",
		Ext:        "log",
		TimeFormat: "2006-01-02",
	})

	if s.cfg.ConfigPath != "" {
		if err := validateConfigPath(s.cfg.ConfigPath); err != nil {
			return err
		}
		config.SetupConfig(s.cfg.ConfigPath)
	} else {
		host, port, err := splitAddress(s.cfg.Address)
		if err != nil {
			return err
		}
		dir := s.cfg.DataDir
		if dir == "" {
			dir = "."
		} else if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create redis data dir %q: %w", dir, err)
		}
		maxClients := s.cfg.MaxClients
		if maxClients <= 0 {
			maxClients = 1000
		}
		config.Properties = &config.ServerProperties{
			Bind:       host,
			Port:       port,
			Dir:        dir,
			AppendOnly: false,
			MaxClients: maxClients,
			RunID:      utils.RandString(40),
		}
	}

	if s.cfg.ConfigPath != "" && s.cfg.Address != "" {
		host, port, err := splitAddress(s.cfg.Address)
		if err != nil {
			return err
		}
		config.Properties.Bind = host
		config.Properties.Port = port
	}

	if config.Properties == nil {
		return fmt.Errorf("redis server properties not initialized")
	}
	if strings.TrimSpace(config.Properties.Dir) == "" {
		config.Properties.Dir = "."
	}
	if config.Properties.Dir != "." {
		if err := os.MkdirAll(config.Properties.Dir, 0o755); err != nil {
			return fmt.Errorf("ensure redis working dir %q: %w", config.Properties.Dir, err)
		}
	}
	if config.Properties.MaxClients <= 0 {
		config.Properties.MaxClients = 1000
	}
	if strings.TrimSpace(config.Properties.RunID) == "" {
		config.Properties.RunID = utils.RandString(40)
	}
	config.Properties.RequirePass = s.cfg.Password
	s.applyAOFSettings()

	// Ensure address reflects final bind/port.
	if strings.TrimSpace(s.cfg.Address) == "" {
		s.cfg.Address = composeAddress(config.Properties.Bind, config.Properties.Port)
	} else {
		host, port, err := splitAddress(s.cfg.Address)
		if err != nil {
			return err
		}
		s.cfg.Address = composeAddress(host, port)
	}

	return nil
}

func (s *Service) applyAOFSettings() {
	if config.Properties == nil {
		return
	}
	aofEnabled := s.cfg.AOFEnabled
	if aofEnabled != nil {
		config.Properties.AppendOnly = *aofEnabled
	}
	if !config.Properties.AppendOnly {
		return
	}
	filename := strings.TrimSpace(s.cfg.AOFFilename)
	if filename == "" {
		filename = "appendonly.aof"
	}
	if !filepath.IsAbs(filename) {
		dir := strings.TrimSpace(config.Properties.Dir)
		if dir == "" {
			dir = "."
		}
		filename = filepath.Join(dir, filename)
	}
	config.Properties.AppendFilename = filename
	fsync := strings.ToLower(strings.TrimSpace(s.cfg.AOFFsync))
	switch fsync {
	case aof.FsyncAlways, aof.FsyncEverySec, aof.FsyncNo:
		config.Properties.AppendFsync = fsync
	default:
		config.Properties.AppendFsync = aof.FsyncEverySec
	}
	if s.cfg.AOFUseRdbPreamble != nil {
		config.Properties.AofUseRdbPreamble = *s.cfg.AOFUseRdbPreamble
	}
}

func splitAddress(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid redis address %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid redis port in address %q: %w", addr, err)
	}
	return host, port, nil
}

func composeAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func validateConfigPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("redis config path %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("redis config path %q is a directory", path)
	}
	return nil
}
