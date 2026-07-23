package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

// SpanID/TraceID are the slog/audit correlation helpers that #850's hub.go
// span_id injection depends on. Pin their behavior: a valid span context in ctx
// yields non-empty ids; a background/nil ctx yields "".

func TestTraceID_SpanID_FromValidSpanContext(t *testing.T) {
	t.Parallel()
	tid, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex("b7ad6b7169203331")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid,
		SpanID:  sid,
		Remote:  true,
	})
	require.True(t, sc.IsValid())
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	require.Equal(t, "0af7651916cd43dd8448eb211c80319c", TraceID(ctx))
	require.Equal(t, "b7ad6b7169203331", SpanID(ctx))
}

func TestTraceID_SpanID_EmptyWhenNoSpan(t *testing.T) {
	t.Parallel()
	require.Empty(t, TraceID(context.Background()))
	require.Empty(t, SpanID(context.Background()))
	// nil ctx must not panic (the helpers guard it); use a typed nil to avoid
	// staticcheck SA1012 while still exercising the nil branch.
	var nilCtx context.Context
	require.Empty(t, TraceID(nilCtx))
	require.Empty(t, SpanID(nilCtx))
}
