package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"

	"backend-server/internal/config"
	"backend-server/internal/interfaces"
)

type tracerManager struct {
	tracer   trace.Tracer
	shutdown func(context.Context) error
}

var (
	globalTracer      = trace.NewNoopTracerProvider().Tracer("xiaozhi/noop")
	tracerManagerOnce sync.Once
	managerInstance   *tracerManager
)

// InitTracing 初始化 OpenTelemetry Trace Provider。
func InitTracing(cfg *config.AppConfig, logger interfaces.Logger) (func(context.Context) error, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	tracingCfg := cfg.Telemetry.Tracing
	if !tracingCfg.Enable {
		logger.Infof("tracing disabled")
		return nil, nil
	}

	var (
		exp sdktrace.SpanExporter
		err error
	)

	switch tracingCfg.Exporter {
	case "otlp":
		clientOpts := []otlptracegrpc.Option{}
		if tracingCfg.Endpoint != "" {
			clientOpts = append(clientOpts, otlptracegrpc.WithEndpoint(tracingCfg.Endpoint))
		}
		if tracingCfg.Insecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}
		client := otlptracegrpc.NewClient(clientOpts...)
		exp, err = otlptrace.New(context.Background(), client)
	case "stdout", "":
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	default:
		return nil, fmt.Errorf("unsupported tracing exporter: %s", tracingCfg.Exporter)
	}
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	serviceName := tracingCfg.ServiceName
	if serviceName == "" {
		serviceName = "backend-server"
	}

	res, err := resource.New(
		context.Background(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			attribute.String("service.environment", tracingCfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.AlwaysSample())
	if tracingCfg.SampleRatio > 0 && tracingCfg.SampleRatio < 1 {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(tracingCfg.SampleRatio))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	shutdown := func(ctx context.Context) error {
		flushTimeout := time.Duration(tracingCfg.FlushTimeout) * time.Millisecond
		if flushTimeout <= 0 {
			flushTimeout = 5 * time.Second
		}

		ctx, cancel := context.WithTimeout(ctx, flushTimeout)
		defer cancel()

		if err := tp.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown tracer provider: %w", err)
		}
		return nil
	}

	tracerManagerOnce.Do(func() {
		managerInstance = &tracerManager{
			tracer:   tp.Tracer("backend-server/chat"),
			shutdown: shutdown,
		}
		globalTracer = managerInstance.tracer
	})

	logger.Infof("tracing exporter initialized: %s", tracingCfg.Exporter)
	return shutdown, nil
}

// Tracer 返回全局 Tracer。
func Tracer() trace.Tracer {
	return globalTracer
}
