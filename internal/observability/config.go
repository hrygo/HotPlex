// Package observability provides unified OpenTelemetry initialization for HotPlex Gateway.
// It replaces the previous split internal/metrics/ and internal/tracing/ packages with a
// single, centrally managed OTel setup.
//
// Application code imports this package and calls Init() once at startup.
// Metrics are created via Meter() and traces via Tracer().
// The Prometheus exporter serves /admin/metrics in standard Prometheus format.
package observability

import (
	"os"
	"time"
)

// Config holds all observability configuration.
// Defaults are set by DefaultConfig().
type Config struct {
	// ServiceName is the OTel service.name resource attribute.
	// Default: "hotplex-gateway".
	ServiceName string

	// ServiceVersion is the OTel service.version resource attribute.
	// Should be set to the binary version at compile time.
	ServiceVersion string

	// Environment is the deployment environment (production/staging/development).
	// Default: read from DEPLOY_ENV or OTEL_RESOURCE_ATTRIBUTES env var.
	Environment string

	// SampleRate is the trace sampling ratio for root spans (0.0 to 1.0).
	// Child spans respect parent sampling decisions (ParentBased).
	// Default: 0.1 (10%).
	SampleRate float64

	// OTLPEndpoint is the gRPC endpoint for OTLP export.
	// Default: read from OTEL_EXPORTER_OTLP_ENDPOINT env var, or empty (disabled).
	OTLPEndpoint string

	// OTLPInsecure disables TLS for the OTLP connection.
	// Default: true for local collectors.
	OTLPInsecure bool

	// OTLPCompression enables gzip compression for OTLP export.
	// Default: true.
	OTLPCompression bool

	// PrometheusEnabled enables the Prometheus metrics endpoint.
	// Default: true.
	PrometheusEnabled bool

	// MaxExportBatchSize is the max batch size for trace export.
	// Default: 512.
	MaxExportBatchSize int

	// BatchTimeout is the max time between trace export batches.
	// Default: 5s.
	BatchTimeout time.Duration

	// MetricInterval is the interval for periodic metric export via OTLP.
	// Default: 30s.
	MetricInterval time.Duration

	// CardinalityLimit is the max number of unique metric time series.
	// Default: 5000.
	CardinalityLimit int
}

// DefaultConfig returns a Config populated with sensible defaults.
// Environment variables (DEPLOY_ENV, OTEL_EXPORTER_OTLP_ENDPOINT) are read
// automatically; callers may override any field before calling Init().
func DefaultConfig() Config {
	env := os.Getenv("DEPLOY_ENV")
	if env == "" {
		env = "development"
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	return Config{
		ServiceName:        "hotplex-gateway",
		Environment:        env,
		SampleRate:         0.1,
		OTLPEndpoint:       endpoint,
		OTLPInsecure:       true,
		OTLPCompression:    true,
		PrometheusEnabled:  true,
		MaxExportBatchSize: 512,
		BatchTimeout:       5 * time.Second,
		MetricInterval:     30 * time.Second,
		CardinalityLimit:   5000,
	}
}

// IsOTELSDKDisabled returns true when OTEL_SDK_DISABLED is set to "true".
// When disabled, all providers are no-op and Init() returns immediately.
func IsOTELSDKDisabled() bool {
	return os.Getenv("OTEL_SDK_DISABLED") == "true"
}
