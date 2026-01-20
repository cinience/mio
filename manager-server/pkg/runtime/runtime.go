package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"manager-server/internal/api"
	"manager-server/internal/config"
	"manager-server/internal/logger"

	"gorm.io/gorm"
)

// Server wraps the manager API runtime to support coordinated lifecycle control.
type Server struct {
	db      *gorm.DB
	primary *http.Server
}

// New bootstraps the manager server stack using its standalone configuration.
func New(configPath string) (*Server, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load manager config: %w", err)
	}

	if err := logger.Setup(cfg.Log); err != nil {
		return nil, fmt.Errorf("configure manager logger: %w", err)
	}

	db, err := config.InitDB(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("init manager database: %w", err)
	}

	srv := api.NewServer(cfg, db)

	primaryAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	primary := &http.Server{
		Addr:    primaryAddr,
		Handler: srv.Router(),
	}

	return &Server{
		db:      db,
		primary: primary,
	}, nil
}

// Run blocks while the manager HTTP server is serving.
func (s *Server) Run() error {
	if s == nil || s.primary == nil {
		return nil
	}

	logger.Infof("Server starting on %s", s.primary.Addr)

	err := s.primary.ListenAndServe()

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// Addr returns the primary HTTP server address.
func (s *Server) Addr() string {
	if s == nil || s.primary == nil {
		return ""
	}
	return s.primary.Addr
}

// Shutdown gracefully stops the HTTP servers and releases database resources.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	var shutdownErr error

	record := func(err error) {
		if err == nil {
			return
		}
		if shutdownErr == nil {
			shutdownErr = err
		} else {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}

	if s.primary != nil {
		record(ignoreServerClosed(s.primary.Shutdown(ctx)))
	}

	if s.db != nil {
		if sqlDB, err := s.db.DB(); err != nil {
			record(err)
		} else {
			record(sqlDB.Close())
		}
	}

	return shutdownErr
}

func ignoreServerClosed(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
