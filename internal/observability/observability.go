package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

var (
	initOnce       sync.Once
	globalMeter    metric.Meter
	globalTracer   trace.Tracer
	globalShutdown func(context.Context) error

	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider
)

var noopMeter = noop.NewMeterProvider().Meter("hotplex-gateway")
var noopTracer = nooptrace.NewTracerProvider().Tracer("hotplex-gateway")

func Init(ctx context.Context, log *slog.Logger, cfg Config) func(context.Context) error {
	initOnce.Do(func() {
		globalShutdown = func(context.Context) error { return nil }

		if IsOTELSDKDisabled() {
			globalMeter = noopMeter
			globalTracer = noopTracer
			log.Info("observability: disabled via OTEL_SDK_DISABLED=true")
			return
		}

		res, err := buildResource(ctx, cfg)
		if err != nil {
			log.Warn("observability: resource creation failed, using no-op", "err", err)
			globalMeter = noopMeter
			globalTracer = noopTracer
			return
		}

		tp, globalTracer, globalShutdown = initTracing(ctx, log, cfg, res)
		mp, globalMeter = initMetrics(ctx, log, cfg, res)

		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		log.Info("observability: initialized",
			"service", cfg.ServiceName,
			"version", cfg.ServiceVersion,
			"environment", cfg.Environment,
			"sample_rate", cfg.SampleRate,
		)
	})
	return globalShutdown
}

func Meter() metric.Meter {
	if globalMeter == nil {
		return noopMeter
	}
	return globalMeter
}

func Tracer() trace.Tracer {
	if globalTracer == nil {
		return noopTracer
	}
	return globalTracer
}

func Shutdown(ctx context.Context) error {
	var errs []error
	if tp != nil {
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
		}
	}
	if mp != nil {
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("observability shutdown errors: %v", errs)
	}
	return nil
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	hostname, _ := os.Hostname()

	attrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceNamespace("hotplex"),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithOS(),
	}

	if cfg.ServiceVersion != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceVersion(cfg.ServiceVersion)))
	}
	if hostname != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceInstanceID(hostname)))
	}

	return resource.New(ctx, attrs...)
}

func initTracing(ctx context.Context, log *slog.Logger, cfg Config, res *resource.Resource) (
	*sdktrace.TracerProvider, trace.Tracer, func(context.Context) error,
) {
	noopSDK := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	noopTracer := noopSDK.Tracer(cfg.ServiceName)
	if cfg.OTLPEndpoint == "" {
		log.Info("observability: no OTEL_EXPORTER_OTLP_ENDPOINT, tracing disabled")
		return noopSDK, noopTracer, noopSDK.Shutdown
	}

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithCompressor("gzip"),
	)
	if err != nil {
		log.Warn("observability: OTLP trace exporter creation failed", "err", err)
		return noopSDK, noopTracer, noopSDK.Shutdown
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
			sdktrace.WithBatchTimeout(cfg.BatchTimeout),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	tracer := tp.Tracer(cfg.ServiceName)
	return tp, tracer, tp.Shutdown
}

func initMetrics(ctx context.Context, log *slog.Logger, cfg Config, res *resource.Resource) (
	*sdkmetric.MeterProvider, metric.Meter,
) {
	opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if cfg.CardinalityLimit > 0 {
		opts = append(opts, sdkmetric.WithCardinalityLimit(cfg.CardinalityLimit))
	}

	if cfg.PrometheusEnabled {
		promExp, err := otelprom.New(
			otelprom.WithoutScopeInfo(),
			otelprom.WithoutTargetInfo(),
		)
		if err != nil {
			log.Warn("observability: Prometheus exporter creation failed", "err", err)
		} else {
			opts = append(opts, sdkmetric.WithReader(promExp))
		}
	}

	if cfg.OTLPEndpoint != "" {
		metricExp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(),
			otlpmetricgrpc.WithCompressor("gzip"),
		)
		if err != nil {
			log.Warn("observability: OTLP metric exporter creation failed", "err", err)
		} else {
			opts = append(opts,
				sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
					sdkmetric.WithInterval(cfg.MetricInterval),
				)),
			)
		}
	}

	mp := sdkmetric.NewMeterProvider(opts...)

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		log.Warn("observability: runtime metrics start failed", "err", err)
	}

	meter := mp.Meter(cfg.ServiceName)
	return mp, meter
}

// MetricsHandler returns an http.Handler that serves Prometheus metrics
// from the OTel MeterProvider. The OTel Prometheus exporter internally
// uses prometheus/client_golang; this function provides the isolation
// boundary so application code never imports prometheus directly.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
