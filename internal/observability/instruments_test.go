package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestStreamingTerminalFailuresInstrument(t *testing.T) {
	t.Parallel()

	// The accessor must be safe before Init: Meter() supplies a noop meter and
	// sync.Once caches the lazily-created instrument for production callers.
	require.NotNil(t, StreamingTerminalFailures())
}

func TestStreamingTerminalFailuresInstrument_RecordsFallbackResult(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	counter, err := newStreamingTerminalFailures(provider.Meter("test"))
	require.NoError(t, err)
	counter.Add(context.Background(), 1, metric.WithAttributes(attribute.String("fallback_result", "failed")))
	counter.Add(context.Background(), 1, metric.WithAttributes(attribute.String("fallback_result", "skipped_body_presented")))

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "hotplex.streaming.terminal_failures" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 2)
			results := make(map[string]int64, len(sum.DataPoints))
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value("fallback_result")
				require.True(t, ok)
				results[value.AsString()] = point.Value
			}
			require.Equal(t, map[string]int64{"failed": 1, "skipped_body_presented": 1}, results)
			return
		}
	}
	t.Fatal("terminal failures metric was not collected")
}
