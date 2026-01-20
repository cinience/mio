package main

import (
	"flag"
	"log/slog"
	"os"

	"aider-server/internal/runnerclient"
)

func main() {
	var configPath string
	var serverAddr string
	var runnerID string
	flag.StringVar(&configPath, "config", "", "path to config yaml (optional)")
	flag.StringVar(&serverAddr, "server", "", "aider-server address (override runner.server_addr)")
	flag.StringVar(&runnerID, "runner-id", "", "runner id (override runner.runner_id)")
	flag.Parse()

	if err := runnerclient.RunDaemon(configPath, serverAddr, runnerID); err != nil {
		slog.Error("runner stopped", "error", err)
		os.Exit(1)
	}
}
