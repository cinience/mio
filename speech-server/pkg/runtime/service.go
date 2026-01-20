package runtime

import (
	"context"
	"fmt"
	"sync"

	serverapp "speech-server/internal/app"
	config "speech-server/internal/config"
	"speech-server/internal/logger"
)

// Service wraps the speech server application lifecycle so it can be embedded.
type Service struct {
	cfg config.Config
	app *serverapp.Application

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	errCh   chan error
}

// New loads the configuration from the provided path and prepares the service.
func New(configPath string) (*Service, error) {
	cfg, err := config.Load(configPath, "")
	if err != nil {
		return nil, err
	}
	return NewWithConfig(cfg)
}

// NewWithConfig constructs a Service from an already parsed Config.
func NewWithConfig(cfg config.Config) (*Service, error) {
	if err := logger.Setup(cfg.Log); err != nil {
		return nil, fmt.Errorf("configure logger: %w", err)
	}
	app, err := serverapp.NewApplication(cfg)
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg: cfg,
		app: app,
	}, nil
}

// Start begins serving traffic in the background.
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("speech service already running")
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.errCh = make(chan error, 1)
	s.running = true

	go func() {
		err := s.app.RunContext(runCtx)
		s.errCh <- err
	}()

	return nil
}

// Shutdown attempts to stop the service and waits for it to exit or the
// provided context to expire.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	cancel := s.cancel
	errCh := s.errCh
	s.running = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if errCh == nil {
		return nil
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Addr returns the configured listen address.
func (s *Service) Addr() string {
	return s.cfg.Addr
}

// ConfigPath exposes the resolved config file path, if any.
func (s *Service) ConfigPath() string {
	return s.cfg.ConfigPath
}
