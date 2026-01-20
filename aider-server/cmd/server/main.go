package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aider-server/internal/api"
	"aider-server/internal/config"
	"aider-server/internal/redisservice"
	"aider-server/internal/runner"
	"aider-server/internal/runnerclient"
	"aider-server/internal/runnerpb"
	"aider-server/internal/store"
	"aider-server/internal/task"
	"aider-server/internal/task/adapters"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

func main() {
	var configPath string
	var runnerDaemon bool
	var runnerServer string
	var runnerID string
	var redisAddrOverride string
	var redisDBOverride int
	var redisPasswordOverride string
	flag.StringVar(&configPath, "config", "", "path to config yaml (optional)")
	flag.BoolVar(&runnerDaemon, "runner-daemon", false, "run as runner daemon")
	flag.StringVar(&runnerServer, "runner-server", "", "override runner server addr")
	flag.StringVar(&runnerID, "runner-id", "", "override runner id")
	flag.StringVar(&redisAddrOverride, "redis-addr", "", "override redis addr (disables embedded redis)")
	flag.IntVar(&redisDBOverride, "redis-db", 0, "override redis db (disables embedded redis when addr is set)")
	flag.StringVar(&redisPasswordOverride, "redis-password", "", "override redis password")
	flag.Parse()

	if runnerDaemon {
		if err := runnerclient.RunDaemon(configPath, runnerServer, runnerID); err != nil {
			slog.Error("runner daemon stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	applyRedisOverrides(cfg, redisAddrOverride, redisDBOverride, redisPasswordOverride)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var redisSvc *redisservice.Service
	redisAddr := cfg.Redis.Addr
	if cfg.Redis.Embedded.Enabled {
		if err := ensureEmbeddedRedisLoopback(cfg.Redis.Embedded.Addr); err != nil {
			slog.Error("embedded redis addr invalid", "error", err)
			os.Exit(1)
		}
		svc, err := redisservice.New(redisservice.Config{
			Address:           cfg.Redis.Embedded.Addr,
			ConfigPath:        cfg.Redis.Embedded.ConfigPath,
			LogDir:            cfg.Redis.Embedded.LogDir,
			DataDir:           cfg.Redis.Embedded.DataDir,
			MaxClients:        cfg.Redis.Embedded.MaxClients,
			Password:          cfg.Redis.Embedded.Password,
			AOFEnabled:        cfg.Redis.Embedded.AOFEnabled,
			AOFFilename:       cfg.Redis.Embedded.AOFFilename,
			AOFFsync:          cfg.Redis.Embedded.AOFFsync,
			AOFUseRdbPreamble: cfg.Redis.Embedded.AOFUseRdbPreamble,
		})
		if err != nil {
			slog.Error("init embedded redis failed", "error", err)
			os.Exit(1)
		}
		if err := svc.Start(); err != nil {
			slog.Error("start embedded redis failed", "error", err)
			os.Exit(1)
		}
		redisSvc = svc
		redisAddr = svc.Address()
		if cfg.Redis.Password == "" {
			cfg.Redis.Password = cfg.Redis.Embedded.Password
		}
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}

	registry := adapters.NewRegistry()
	for name, adapterCfg := range cfg.Adapters {
		registry.Register(adapters.Adapter{
			Name:    name,
			Command: adapterCfg.Command,
			Args:    adapterCfg.Args,
		})
	}

	retention := time.Duration(cfg.LogRetentionSeconds) * time.Second
	var (
		manager    task.Service
		grpcServer *grpc.Server
	)
	if strings.EqualFold(cfg.ExecutionMode, "runner") {
		runnerManager := runner.NewManager(
			store.NewRedisStore(redisClient, cfg.Redis.KeyPrefix),
			registry,
			cfg.AllowedBinaries,
			cfg.DataDir,
			retention,
			cfg.Runner.LogBackfillBatchSize,
			cfg.Runner.AuthToken,
			cfg.Runner.LaunchCommand,
			cfg.Runner.LaunchArgs,
			cfg.Runner.LaunchTimeoutSeconds,
			cfg.Runner.LaunchDetach,
			cfg.Runner.ServerAddr,
			configPath,
			cfg.Codex.DefaultProtocol,
		)
		manager = runnerManager
		grpcServer = grpc.NewServer()
		runnerpb.RegisterRunnerControlServer(grpcServer, runner.NewBridge(runnerManager))
	} else {
		manager = task.NewManager(
			store.NewRedisStore(redisClient, cfg.Redis.KeyPrefix),
			registry,
			cfg.AllowedBinaries,
			cfg.DataDir,
			retention,
			task.CodexProtocolConfig{
				DefaultProtocol: cfg.Codex.DefaultProtocol,
				AppServer: task.CodexAppServerConfig{
					Command: cfg.Codex.AppServer.Command,
					Args:    cfg.Codex.AppServer.Args,
					Env:     cfg.Codex.AppServer.Env,
				},
			},
		)
	}
	if err := manager.RebuildIndexes(ctx); err != nil {
		slog.Error("rebuild indexes failed", "error", err)
	}

	metrics := api.NewMetrics()
	metrics.StartTaskCollector(ctx, manager, 10*time.Second)

	srv := api.NewServer(manager, metrics, cfg.Auth.Token, cfg.DefaultEnv)
	handler := srv.Handler()
	if grpcServer != nil {
		handler = runner.GRPCHandler(grpcServer, handler)
	}
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go manager.CleanupExpired(ctx, 30*time.Second)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = redisClient.Close()
		if redisSvc != nil {
			_ = redisSvc.Shutdown(shutdownCtx)
		}
	}()

	slog.Info("aider-server starting", "addr", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func applyRedisOverrides(cfg *config.Config, addr string, db int, password string) {
	envAddr := strings.TrimSpace(os.Getenv("AIDER_REDIS_ADDR"))
	if addr == "" {
		addr = envAddr
	}
	if addr != "" {
		cfg.Redis.Addr = addr
		cfg.Redis.Embedded.Enabled = false
	}
	if rawDB := strings.TrimSpace(os.Getenv("AIDER_REDIS_DB")); rawDB != "" && db == 0 {
		if parsed, err := strconv.Atoi(rawDB); err == nil {
			db = parsed
		}
	}
	if db != 0 {
		cfg.Redis.DB = db
	}
	envPassword := strings.TrimSpace(os.Getenv("AIDER_REDIS_PASSWORD"))
	if password == "" {
		password = envPassword
	}
	if password != "" {
		cfg.Redis.Password = password
	}
}

func ensureEmbeddedRedisLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("embedded redis must bind to localhost, got %q", addr)
	}
	return nil
}
