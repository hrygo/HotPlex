package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func newTerminalFailureMetric(t *testing.T) (*sdkmetric.ManualReader, metric.Int64Counter) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	counter, err := provider.Meter("feishu-test").Int64Counter("hotplex.streaming.terminal_failures")
	require.NoError(t, err)
	return reader, counter
}

func terminalFailureResults(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "hotplex.streaming.terminal_failures" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			results := make(map[string]int64, len(sum.DataPoints))
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value("fallback_result")
				require.True(t, ok)
				results[value.AsString()] = point.Value
			}
			return results
		}
	}
	t.Fatal("terminal failures metric was not collected")
	return nil
}

func TestFeishuConn_HandleDone_SendsShortTerminalFallbackWhenBodyNotPresented(t *testing.T) {
	t.Parallel()

	reader, terminalFailures := newTerminalFailureMetric(t)
	var mu sync.Mutex
	var sentBodies []string
	adapter := newTestAdapter(t)
	adapter.Interactions = messaging.NewInteractionManager(discardLogger)
	adapter.larkClient = lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/open-apis/im/v1/messages" {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			mu.Lock()
			sentBodies = append(sentBodies, string(body))
			mu.Unlock()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok"}`)),
			Request:    req,
		}, nil
	})))

	apiErr := errors.New("terminal API unavailable")
	limiter := NewFeishuRateLimiter()
	t.Cleanup(limiter.Stop)
	ctrlClient := lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, apiErr
	})))
	ctrl := NewStreamingCardController(ctrlClient, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
	ctrl.phase.Store(int32(PhaseStreaming))
	ctrl.mu.Lock()
	ctrl.cardID = "card-1"
	ctrl.msgID = "msg-1"
	ctrl.buf.WriteString("full worker response must not be sent again")
	ctrl.mu.Unlock()

	conn := NewFeishuConn(adapter, "chat-1", "", "")
	conn.terminalFailures = terminalFailures
	conn.EnableStreaming(ctrl)
	err := conn.WriteCtx(context.Background(), &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Done, Data: events.DoneData{Success: true}},
	})

	require.ErrorIs(t, err, ErrTerminalDelivery)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sentBodies, 1)
	require.Contains(t, sentBodies[0], terminalDeliveryFallbackText)
	require.NotContains(t, sentBodies[0], "full worker response must not be sent again")
	require.Equal(t, map[string]int64{"sent": 1}, terminalFailureResults(t, reader))
}

func TestFeishuConn_HandleDone_JoinsFallbackFailureWithTerminalDeliveryError(t *testing.T) {
	t.Parallel()

	reader, terminalFailures := newTerminalFailureMetric(t)
	apiErr := errors.New("terminal API unavailable")
	limiter := NewFeishuRateLimiter()
	t.Cleanup(limiter.Stop)
	ctrlClient := lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, apiErr
	})))
	ctrl := NewStreamingCardController(ctrlClient, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
	ctrl.phase.Store(int32(PhaseStreaming))
	ctrl.mu.Lock()
	ctrl.cardID = "card-1"
	ctrl.msgID = "msg-1"
	ctrl.buf.WriteString("full worker response")
	ctrl.mu.Unlock()

	adapter := newTestAdapter(t) // nil lark client makes the static fallback fail.
	adapter.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(adapter, "chat-1", "", "")
	conn.terminalFailures = terminalFailures
	conn.EnableStreaming(ctrl)
	err := conn.WriteCtx(context.Background(), &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Done, Data: events.DoneData{Success: true}},
	})

	require.ErrorIs(t, err, ErrTerminalDelivery)
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "terminal fallback delivery")
	require.ErrorContains(t, err, "lark client not initialized")
	require.Equal(t, map[string]int64{"failed": 1}, terminalFailureResults(t, reader))
}

func TestFeishuConn_HandleDone_SkipsStaticFallbackWhenBodyAlreadyPresented(t *testing.T) {
	t.Parallel()

	reader, terminalFailures := newTerminalFailureMetric(t)
	var mu sync.Mutex
	var sentBodies []string
	adapter := newTestAdapter(t)
	adapter.Interactions = messaging.NewInteractionManager(discardLogger)
	adapter.larkClient = lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/open-apis/im/v1/messages" {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			mu.Lock()
			sentBodies = append(sentBodies, string(body))
			mu.Unlock()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok"}`)),
			Request:    req,
		}, nil
	})))

	limiter := NewFeishuRateLimiter()
	t.Cleanup(limiter.Stop)
	ctrlClient := lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"code":0,"msg":"ok"}`
		if req.Method == http.MethodPut && strings.HasSuffix(req.URL.Path, "/cards/card-1") {
			body = `{"code":999,"msg":"header update failed"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})))
	ctrl := NewStreamingCardController(ctrlClient, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
	ctrl.phase.Store(int32(PhaseStreaming))
	ctrl.mu.Lock()
	ctrl.cardID = "card-1"
	ctrl.msgID = "msg-1"
	ctrl.buf.WriteString("full worker response")
	ctrl.mu.Unlock()

	conn := NewFeishuConn(adapter, "chat-1", "", "")
	conn.terminalFailures = terminalFailures
	conn.EnableStreaming(ctrl)
	err := conn.WriteCtx(context.Background(), &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Done, Data: events.DoneData{Success: true}},
	})

	require.ErrorIs(t, err, ErrTerminalDelivery)
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, sentBodies)
	require.Equal(t, map[string]int64{"skipped_body_presented": 1}, terminalFailureResults(t, reader))
}

func TestFeishuConn_TerminalHandlersShareCallerDeadlineAcrossCloseAndFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind events.Kind
		data any
	}{
		{name: "done", kind: events.Done, data: events.DoneData{Success: true}},
		{name: "error", kind: events.Error, data: events.ErrorData{Code: "MODEL_ERROR", Message: "model failed"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu                sync.Mutex
				closeDeadlines    []time.Time
				fallbackDeadlines []time.Time
			)
			captureDeadline := func(dst *[]time.Time) mediaHTTPClientFunc {
				return func(req *http.Request) (*http.Response, error) {
					deadline, ok := req.Context().Deadline()
					require.True(t, ok, "terminal HTTP request must carry a deadline")
					mu.Lock()
					*dst = append(*dst, deadline)
					mu.Unlock()
					return nil, errors.New("terminal HTTP unavailable")
				}
			}

			limiter := NewFeishuRateLimiter()
			t.Cleanup(limiter.Stop)
			ctrlClient := lark.NewClient("test-app", "test-secret", lark.WithHttpClient(captureDeadline(&closeDeadlines)))
			ctrl := NewStreamingCardController(ctrlClient, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
			ctrl.phase.Store(int32(PhaseStreaming))
			ctrl.mu.Lock()
			ctrl.cardID = "card-1"
			ctrl.msgID = "msg-1"
			ctrl.buf.WriteString("full worker response")
			ctrl.mu.Unlock()

			adapter := newTestAdapter(t)
			adapter.Interactions = messaging.NewInteractionManager(discardLogger)
			adapter.larkClient = lark.NewClient("test-app", "test-secret", lark.WithHttpClient(captureDeadline(&fallbackDeadlines)))
			conn := NewFeishuConn(adapter, "chat-1", "", "")
			conn.EnableStreaming(ctrl)

			callerTimeout := 250 * time.Millisecond
			if tt.kind == events.Error {
				callerTimeout = 6 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), callerTimeout)
			defer cancel()
			callerDeadline, ok := ctx.Deadline()
			require.True(t, ok)
			err := conn.WriteCtx(ctx, &events.Envelope{
				Version:   events.Version,
				SessionID: "session-1",
				Event:     events.Event{Type: tt.kind, Data: tt.data},
			})

			require.ErrorIs(t, err, ErrTerminalDelivery)
			mu.Lock()
			defer mu.Unlock()
			require.NotEmpty(t, closeDeadlines)
			require.NotEmpty(t, fallbackDeadlines)
			for _, deadline := range append(closeDeadlines, fallbackDeadlines...) {
				require.WithinDuration(t, callerDeadline, deadline, 10*time.Millisecond,
					"close and fallback must share the caller's terminal budget")
			}
		})
	}
}

func TestFeishuConn_HandleDone_DefaultTerminalDeadlineIsSharedWithFallback(t *testing.T) {
	t.Parallel()

	var (
		mu                sync.Mutex
		closeDeadlines    []time.Time
		fallbackDeadlines []time.Time
	)
	captureDeadline := func(dst *[]time.Time) mediaHTTPClientFunc {
		return func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			require.True(t, ok, "terminal HTTP request must carry the default deadline")
			mu.Lock()
			*dst = append(*dst, deadline)
			mu.Unlock()
			return nil, errors.New("terminal HTTP unavailable")
		}
	}

	limiter := NewFeishuRateLimiter()
	t.Cleanup(limiter.Stop)
	ctrlClient := lark.NewClient("test-app", "test-secret", lark.WithHttpClient(captureDeadline(&closeDeadlines)))
	ctrl := NewStreamingCardController(ctrlClient, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
	ctrl.phase.Store(int32(PhaseStreaming))
	ctrl.mu.Lock()
	ctrl.cardID = "card-1"
	ctrl.msgID = "msg-1"
	ctrl.buf.WriteString("terminal body")
	ctrl.mu.Unlock()

	adapter := newTestAdapter(t)
	adapter.Interactions = messaging.NewInteractionManager(discardLogger)
	adapter.larkClient = lark.NewClient("test-app", "test-secret", lark.WithHttpClient(captureDeadline(&fallbackDeadlines)))
	conn := NewFeishuConn(adapter, "chat-1", "", "")
	conn.EnableStreaming(ctrl)

	started := time.Now()
	err := conn.WriteCtx(context.Background(), &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Done, Data: events.DoneData{Success: true}},
	})

	require.ErrorIs(t, err, ErrTerminalDelivery)
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, closeDeadlines)
	require.NotEmpty(t, fallbackDeadlines)
	sharedDeadline := closeDeadlines[0]
	require.WithinDuration(t, started.Add(defaultTerminalDeliveryTimeout), sharedDeadline, 50*time.Millisecond)
	for _, deadline := range append(closeDeadlines, fallbackDeadlines...) {
		require.Equal(t, sharedDeadline, deadline, "close and fallback must use one default end-to-end deadline")
	}
}
