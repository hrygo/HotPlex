package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestFeishuConn_HandleDone_SendsShortTerminalFallbackWhenBodyNotPresented(t *testing.T) {
	t.Parallel()

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
}

func TestFeishuConn_HandleDone_JoinsFallbackFailureWithTerminalDeliveryError(t *testing.T) {
	t.Parallel()

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
}

func TestFeishuConn_HandleDone_SkipsStaticFallbackWhenBodyAlreadyPresented(t *testing.T) {
	t.Parallel()

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
}
