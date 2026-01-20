package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"aio-server/internal/redisservice"
	aioruntime "aio-server/pkg/runtime"

	"github.com/urfave/cli/v2"
)

const (
	serviceBackend = "backend"
	serviceManager = "manager"
	serviceRedis   = "redis"
	serviceSpeech  = "speech"
)

var (
	version             = "unknown"
	availableServices   = initAvailableServices()
	defaultSpeechConfig = defaultSpeechConfigPath()
)

func main() {
	app := &cli.App{
		Name:  "aio-server",
		Usage: "Launch backend and manager services together or individually",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "service",
				Usage:   fmt.Sprintf("Service(s) to start (%s). Repeat flag to select multiple; defaults to all", strings.Join(availableServices, ", ")),
				Aliases: []string{"s"},
			},
			&cli.StringFlag{
				Name:  "backend-config",
				Usage: "Path to the backend server config file",
				Value: "../backend-server/config/config.yaml",
			},
			&cli.StringFlag{
				Name:  "manager-config",
				Usage: "Path to the manager server config file",
				Value: "../manager-server/config/config.yaml",
			},
			&cli.StringFlag{
				Name:  "speech-config",
				Usage: "Path to the speech server config file (sherpa_onnx builds)",
				Value: defaultSpeechConfig,
			},
			&cli.StringFlag{
				Name:  "redis-config",
				Usage: "Path to the embedded redis config file (optional)",
			},
			&cli.StringFlag{
				Name:  "redis-address",
				Usage: "Address for the embedded redis service (overrides config bind/port when set)",
				Value: "127.0.0.1:26379",
			},
			&cli.StringFlag{
				Name:  "redis-password",
				Usage: "Password for Redis (overrides backend redis.password when set; empty clears password)",
			},
			&cli.DurationFlag{
				Name:  "health-interval",
				Usage: "Interval between health checks (0 disables)",
				Value: 30 * time.Second,
			},
			&cli.DurationFlag{
				Name:  "health-timeout",
				Usage: "Timeout for individual health checks",
				Value: 5 * time.Second,
			},
			&cli.DurationFlag{
				Name:  "restart-backoff",
				Usage: "Minimum interval between restart attempts per service",
				Value: 15 * time.Second,
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(c *cli.Context) error {
	services, err := resolveServices(c.StringSlice("service"))
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := aioruntime.Config{
		Version:       version,
		BackendConfig: c.String("backend-config"),
		ManagerConfig: c.String("manager-config"),
		SpeechConfig:  c.String("speech-config"),
		Redis: redisservice.Config{
			Address:    c.String("redis-address"),
			ConfigPath: c.String("redis-config"),
			LogDir:     filepath.Join("logs", "redis"),
		},
		RedisAddress:        c.String("redis-address"),
		RedisPassword:       strings.TrimSpace(c.String("redis-password")),
		DeriveRedisPassword: false,
		HealthCheckInterval: c.Duration("health-interval"),
		HealthCheckTimeout:  c.Duration("health-timeout"),
		RestartBackoff:      c.Duration("restart-backoff"),
	}
	if cfg.RedisPassword == "" {
		cfg.DeriveRedisPassword = true
	}

	supervisorEnabled := cfg.HealthCheckInterval > 0
	var managerDone chan error
	if services[serviceManager] {
		if supervisorEnabled {
			cfg.OnManagerRunComplete = func(err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "manager server exited: %v\n", err)
				}
			}
		} else {
			managerDone = make(chan error, 1)
			cfg.OnManagerRunComplete = func(err error) {
				select {
				case managerDone <- err:
				default:
				}
			}
		}
	}

	rt, err := aioruntime.New(cfg)
	if err != nil {
		return cli.Exit(fmt.Sprintf("failed to initialize runtime: %v", err), 1)
	}

	active := selectedServiceList(services)
	if err := rt.Start(ctx, active...); err != nil {
		return cli.Exit(fmt.Sprintf("failed to start services: %v", err), 1)
	}

	var (
		managerRunErr   error
		signalTriggered bool
	)

	switch {
	case managerDone != nil:
		select {
		case <-ctx.Done():
			signalTriggered = true
		case err := <-managerDone:
			managerRunErr = err
			stop()
		}
	default:
		<-ctx.Done()
		signalTriggered = true
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rt.Shutdown(shutdownCtx); err != nil {
		return cli.Exit(fmt.Sprintf("shutdown encountered issues: %v", err), 1)
	}

	if signalTriggered && managerDone != nil && managerRunErr == nil {
		select {
		case err := <-managerDone:
			managerRunErr = err
		default:
		}
	}

	if managerRunErr != nil {
		fmt.Fprintf(os.Stderr, "manager server exited: %v\n", managerRunErr)
	}

	return nil
}

func selectedServiceList(selection map[string]bool) []string {
	names := make([]string, 0, len(selection))
	for name, enabled := range selection {
		if enabled {
			names = append(names, name)
		}
	}
	return names
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
