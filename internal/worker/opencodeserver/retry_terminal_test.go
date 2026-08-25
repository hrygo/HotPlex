package opencodeserver

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

type manualRetryTimer struct {
	stopped bool
}

func (t *manualRetryTimer) Stop() bool {
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

func TestRetryTerminal_ExpiryReturnsOriginalError(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(http.ResponseWriter, *http.Request) {})
	var expire func()
	s.retryTerminal = newRetryTerminalArbiter(func(_ time.Duration, fn func()) retryTimer {
		expire = fn
		return &manualRetryTimer{}
	})
	ch := s.Subscribe("ses_retry_failure")
	t.Cleanup(func() { s.Unsubscribe("ses_retry_failure") })

	dispatch := func(eventType string, props map[string]any) {
		t.Helper()
		data := strings.TrimSpace(strings.TrimPrefix(ocsEvent(t, eventType, props), "data: "))
		s.dispatchOCSEvent([]byte(data))
	}

	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_failure",
		"status": map[string]any{
			"type":    "retry",
			"attempt": 1,
			"message": "Monthly usage limit reached",
		},
	})
	require.Equal(t, events.State, collectN(t, ch, 1)[0].Event.Type)

	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_failure",
		"status":    map[string]any{"type": "idle"},
	})
	require.NotNil(t, expire)
	select {
	case env := <-ch:
		t.Fatalf("unexpected event before retry grace expiry: %s", env.Event.Type)
	default:
	}

	expire()
	got := collectN(t, ch, 2)
	require.Equal(t, events.Error, got[0].Event.Type)
	errData := got[0].Event.Data.(events.ErrorData)
	require.Equal(t, events.ErrCodeRateLimited, errData.Code)
	require.Equal(t, "Monthly usage limit reached", errData.Message)
	require.Equal(t, events.Done, got[1].Event.Type)
	done := got[1].Event.Data.(events.DoneData)
	require.False(t, done.Success)
}

func TestRetryErrorCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    events.ErrorCode
	}{
		{name: "monthly usage limit", message: "Monthly usage limit reached", want: events.ErrCodeRateLimited},
		{name: "http 429", message: "provider returned 429", want: events.ErrCodeRateLimited},
		{name: "generic provider failure", message: "provider connection failed", want: events.ErrCodeInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, retryErrorCode(tt.message))
		})
	}
}

func TestRetryTerminal_FallbackCancelsStaleExpiry(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(http.ResponseWriter, *http.Request) {})
	var expire func()
	s.retryTerminal = newRetryTerminalArbiter(func(_ time.Duration, fn func()) retryTimer {
		expire = fn
		return &manualRetryTimer{}
	})
	ch := s.Subscribe("ses_retry_recovered")
	t.Cleanup(func() { s.Unsubscribe("ses_retry_recovered") })

	dispatch := func(eventType string, props map[string]any) {
		t.Helper()
		data := strings.TrimSpace(strings.TrimPrefix(ocsEvent(t, eventType, props), "data: "))
		s.dispatchOCSEvent([]byte(data))
	}

	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_recovered",
		"status": map[string]any{
			"type":    "retry",
			"message": "provider quota reached",
		},
	})
	require.Equal(t, events.State, collectN(t, ch, 1)[0].Event.Type)
	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_recovered",
		"status":    map[string]any{"type": "idle"},
	})
	require.NotNil(t, expire)

	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_recovered",
		"status":    map[string]any{"type": "busy"},
	})
	require.Equal(t, events.State, collectN(t, ch, 1)[0].Event.Type)

	expire()
	select {
	case env := <-ch:
		t.Fatalf("stale retry expiry emitted event after fallback resumed: %s", env.Event.Type)
	default:
	}
}

func TestRetryTerminal_BroadcastErrorCancelsStaleExpiry(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(http.ResponseWriter, *http.Request) {})
	var expire func()
	s.retryTerminal = newRetryTerminalArbiter(func(_ time.Duration, fn func()) retryTimer {
		expire = fn
		return &manualRetryTimer{}
	})
	ch := s.Subscribe("ses_retry_broadcast_error")
	t.Cleanup(func() { s.Unsubscribe("ses_retry_broadcast_error") })

	dispatch := func(eventType string, props map[string]any) {
		t.Helper()
		data := strings.TrimSpace(strings.TrimPrefix(ocsEvent(t, eventType, props), "data: "))
		s.dispatchOCSEvent([]byte(data))
	}

	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_broadcast_error",
		"status": map[string]any{
			"type":    "retry",
			"message": "provider unavailable",
		},
	})
	require.Equal(t, events.State, collectN(t, ch, 1)[0].Event.Type)
	dispatch(ocsSessionStatus, map[string]any{
		"sessionID": "ses_retry_broadcast_error",
		"status":    map[string]any{"type": "idle"},
	})
	require.NotNil(t, expire)

	dispatch(ocsSessionError, map[string]any{
		"error": map[string]any{
			"name": "ProviderError",
			"data": map[string]any{"message": "provider failed permanently"},
		},
	})
	got := collectN(t, ch, 2)
	require.Equal(t, events.Error, got[0].Event.Type)
	require.Equal(t, "provider failed permanently", got[0].Event.Data.(events.ErrorData).Message)
	require.Equal(t, events.Done, got[1].Event.Type)

	expire()
	select {
	case env := <-ch:
		t.Fatalf("stale retry expiry emitted event after broadcast error: %s", env.Event.Type)
	default:
	}
}
