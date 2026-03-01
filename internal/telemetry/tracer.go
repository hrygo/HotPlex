package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	Sampled        float64
}

type Tracer struct {
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
	enabled  bool
}

var (
	globalTracer *Tracer
	once         sync.Once
)

func NewTracer(cfg Config, logger *slog.Logger) (*Tracer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if cfg.OTLPEndpoint == "" {
		logger.Info("OpenTelemetry disabled: no OTLP endpoint configured")
		return &Tracer{logger: logger, enabled: false}, nil
	}

	ctx := context.Background()

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	))
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	sampleRatio := cfg.Sampled
	if sampleRatio <= 0 {
		sampleRatio = 1.0
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRatio)),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := provider.Tracer(cfg.ServiceName)

	return &Tracer{
		tracer:   tracer,
		provider: provider,
		logger:   logger,
		enabled:  true,
	}, nil
}

func (t *Tracer) StartSession(ctx context.Context, sessionID, namespace string) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, trace.SpanFromContext(ctx)
	}

	ctx, span := t.tracer.Start(ctx, "session.execute",
		trace.WithAttributes(
			attribute.String("session.id", sessionID),
			attribute.String("namespace", namespace),
		),
		trace.WithTimestamp(time.Now()),
	)
	return ctx, span
}

func (t *Tracer) EndSession(span trace.Span, err error) {
	if !t.enabled || span == nil {
		return
	}

	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error", "true"))
	}
	span.End()
}

func (t *Tracer) StartToolUse(ctx context.Context, toolName, toolID string) (context.Context, trace.Span) {
	if !t.enabled {
		return ctx, trace.SpanFromContext(ctx)
	}

	ctx, span := t.tracer.Start(ctx, "tool.use",
		trace.WithAttributes(
			attribute.String("tool.name", toolName),
			attribute.String("tool.id", toolID),
		),
	)
	return ctx, span
}

func (t *Tracer) RecordDangerBlock(ctx context.Context, operation, reason string) {
	if !t.enabled {
		return
	}

	_, span := t.tracer.Start(ctx, "security.danger_block",
		trace.WithAttributes(
			attribute.String("danger.operation", operation),
			attribute.String("danger.reason", reason),
			attribute.String("security.level", "blocked"),
		),
	)
	span.End()
}

func (t *Tracer) Close(ctx context.Context) error {
	if !t.enabled || t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

func (t *Tracer) Enabled() bool {
	return t.enabled
}

func Init(cfg Config, logger *slog.Logger) error {
	var initErr error
	once.Do(func() {
		globalTracer, initErr = NewTracer(cfg, logger)
	})
	return initErr
}

func Get() *Tracer {
	if globalTracer == nil {
		return &Tracer{enabled: false}
	}
	return globalTracer
}
