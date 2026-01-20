package main

import (
	"log"

	serverapp "speech-server/internal/app"
	"speech-server/internal/logger"
)

func main() {
	cfg, err := serverapp.ParseFlags()
	if err != nil {
		log.Fatalf("failed to parse flags: %v", err)
	}

	if err := logger.Setup(cfg.Log); err != nil {
		log.Fatalf("failed to configure logger: %v", err)
	}

	app, err := serverapp.NewApplication(cfg)
	if err != nil {
		logger.Fatalf("failed to initialize application: %v", err)
	}

	if err := app.Run(); err != nil {
		logger.Fatalf("server exited with error: %v", err)
	}
}
