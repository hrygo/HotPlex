package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

func TestPopulateForwardSessionContextWithoutEventCollector(t *testing.T) {
	t.Parallel()

	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)
	sm.On("Get", "sess-trace-only").Return(&session.SessionInfo{
		ID:          "sess-trace-only",
		UserID:      "user1",
		WorkspaceID: "ws1",
		Platform:    "webchat",
		WorkerType:  worker.TypeClaudeCode,
	}, nil).Once()

	b := &Bridge{sm: sm} // collector intentionally nil: tracing must still enrich.
	fc := &forwardContext{}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	_, span := provider.Tracer("test").Start(context.Background(), "forward")
	b.populateForwardSessionContext(
		span, fc, "sess-trace-only", worker.TypeClaudeCode,
	)
	span.End()

	require.Equal(t, "webchat", fc.sessPlatform)
	require.Equal(t, "user1", fc.sessOwner)
	require.Len(t, recorder.Ended(), 1)
	attrs := make(map[string]string)
	for _, attr := range recorder.Ended()[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	require.Equal(t,
		agentspec.DeriveAgentID("user1", "ws1", string(worker.TypeClaudeCode), string(worker.TypeClaudeCode)),
		attrs[agentspec.MetadataKeyAgentID],
	)
	require.Equal(t, "user1", attrs[agentspec.MetadataKeyUserID])
	require.Equal(t, "ws1", attrs[agentspec.MetadataKeyWorkspaceID])
	require.Equal(t, "webchat", attrs[agentspec.MetadataKeyPlatform])
	require.Equal(t, string(worker.TypeClaudeCode), attrs[agentspec.MetadataKeyWorkerType])
	sm.AssertExpectations(t)
}
