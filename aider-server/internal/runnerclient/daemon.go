package runnerclient

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"aider-server/internal/config"
)

func RunDaemon(configPath, serverAddr, runnerID string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if serverAddr != "" {
		cfg.Runner.ServerAddr = serverAddr
	}
	if runnerID != "" {
		cfg.Runner.RunnerID = runnerID
	}
	if cfg.Runner.ServerAddr == "" {
		return errMissingServerAddr
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner, err := NewRunner(cfg.Runner, cfg.DataDir)
	if err != nil {
		return err
	}
	if err := runner.Run(ctx); err != nil {
		slog.Error("runner stopped", "error", err)
		return err
	}
	return nil
}

var errMissingServerAddr = func() error {
	return &configError{message: "runner server_addr is required"}
}()

type configError struct {
	message string
}

func (e *configError) Error() string {
	return e.message
}
