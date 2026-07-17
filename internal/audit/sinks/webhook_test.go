package sinks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWebhookSink_DeliversAndSigns verifies a delivered event is POSTed with a
// valid HMAC-SHA256 signature when a secret is configured.
func TestWebhookSink_DeliversAndSigns(t *testing.T) {
	t.Parallel()
	secret := "test-secret-32-bytes-long-aaaaaa"
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotSig   string
		gotCT    string
		requests int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = b
		gotSig = r.Header.Get("X-Audit-Signature")
		gotCT = r.Header.Get("Content-Type")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{
		"url": srv.URL, "secret": secret,
	}, slog.Default())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	err = s.OnAlertEvent(context.Background(), AlertEvent{
		EventID: "e1", UserID: "u1", Action: "auth.login", Outcome: "success",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool { return atomic.LoadInt32(&requests) >= 1 },
		2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return s.Delivered() >= 1 },
		1*time.Second, 5*time.Millisecond, "delivery counter must reflect async POST success")
	require.Equal(t, int64(0), s.Failures())

	// Verify body is valid JSON carrying the event.
	mu.Lock()
	defer mu.Unlock()
	var got AlertEvent
	require.NoError(t, json.Unmarshal(gotBody, &got))
	require.Equal(t, "e1", got.EventID)
	require.Equal(t, "application/json", gotCT)
	// Verify HMAC signature matches the body + secret.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	require.Equal(t, wantSig, gotSig)
}

// TestWebhookSink_NoSecretOmitsSignature verifies that without a secret, no
// X-Audit-Signature header is sent (unsigned mode for trusted internal nets).
func TestWebhookSink_NoSecretOmitsSignature(t *testing.T) {
	t.Parallel()
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Audit-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{"url": srv.URL}, slog.Default())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	require.NoError(t, s.OnAlertEvent(context.Background(), AlertEvent{EventID: "e2"}))
	require.Eventually(t, func() bool { return s.Delivered() >= 1 }, 2*time.Second, 5*time.Millisecond)
	require.Empty(t, gotSig, "no signature header when secret unset")
}

// TestWebhookSink_ServerErrorRetriesThenDrops verifies a server returning 5xx
// triggers retries; after exhausting attempts the event is dropped (Failures++
// ) without blocking OnAlertEvent.
func TestWebhookSink_ServerErrorRetriesThenDrops(t *testing.T) {
	t.Parallel()
	// Tight retry base so the test stays under the per-module budget. We can't
	// override the package const, so this test is sensitive to webhookRetryBaseDelay
	// (1s) + webhookRetryAttempts (3): worst case ~1+2 = 3s. Use Eventually with
	// a generous window.
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{"url": srv.URL}, slog.Default())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	require.NoError(t, s.OnAlertEvent(context.Background(), AlertEvent{EventID: "e-fail"}))
	// 3 attempts with 1s/2s backoff → ~3s. Wait up to 8s.
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&attempts) >= webhookRetryAttempts && s.Failures() >= 1
	}, 8*time.Second, 20*time.Millisecond)
	require.Equal(t, int32(webhookRetryAttempts), atomic.LoadInt32(&attempts),
		"exactly webhookRetryAttempts attempts before drop")
	require.Equal(t, int64(1), s.Failures())
	require.Equal(t, int64(0), s.Delivered())
}

// TestWebhookSink_QueueFullDrops verifies OnAlertEvent does NOT block when the
// internal queue is full — the event is dropped and Failures++ (spec §5.6
// backpressure: a slow sink must not stall the audit write path).
func TestWebhookSink_QueueFullDrops(t *testing.T) {
	t.Parallel()
	// Server that never responds quickly → queue backs up. Use a server that
	// sleeps so the single delivery goroutine stays busy.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-time.After(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{
		"url": srv.URL, "queue_cap": 2,
	}, slog.Default())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	// First event grabs the delivery goroutine (sleeps 500ms). Next 2 fill the
	// queue (cap 2). The 4th must be dropped, not block.
	for i := 0; i < 4; i++ {
		start := time.Now()
		err := s.OnAlertEvent(context.Background(), AlertEvent{EventID: "e"})
		elapsed := time.Since(start)
		require.NoError(t, err)
		require.Less(t, elapsed, 100*time.Millisecond,
			"OnAlertEvent must not block even when queue is full")
	}
	// At least one drop recorded (queue cap 2 + 1 in-flight = 3 capacity; 4th dropped).
	require.Eventually(t, func() bool { return s.Failures() >= 1 },
		2*time.Second, 5*time.Millisecond)
}

// TestWebhookSink_RetrySucceeds verifies transient failure followed by success
// delivers the event (not dropped).
func TestWebhookSink_RetrySucceeds(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // first attempt fails
			return
		}
		w.WriteHeader(http.StatusOK) // retry succeeds
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{"url": srv.URL}, slog.Default())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close(context.Background())) }()

	require.NoError(t, s.OnAlertEvent(context.Background(), AlertEvent{EventID: "e-retry"}))
	require.Eventually(t, func() bool { return s.Delivered() >= 1 },
		8*time.Second, 10*time.Millisecond)
	require.Equal(t, int64(0), s.Failures())
	require.GreaterOrEqual(t, atomic.LoadInt32(&attempts), int32(2))
}

func TestWebhookSink_CloseDrainsQueue(t *testing.T) {
	t.Parallel()
	var delivered atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := NewWebhookSink(map[string]any{"url": srv.URL, "queue_cap": 8}, slog.Default())
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, s.OnAlertEvent(context.Background(), AlertEvent{EventID: "drain"}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Close(ctx))
	require.Equal(t, int32(5), delivered.Load())
	require.Error(t, s.OnAlertEvent(context.Background(), AlertEvent{EventID: "closed"}))
}

// TestWebhookSink_ConfigErrors covers required-field validation.
func TestWebhookSink_ConfigErrors(t *testing.T) {
	t.Parallel()
	t.Run("missing url", func(t *testing.T) {
		t.Parallel()
		_, err := NewWebhookSink(map[string]any{}, slog.Default())
		require.Error(t, err)
		require.Contains(t, err.Error(), "url")
	})
}

// TestWebhookSink_BuildViaRegistry verifies the webhook factory is registered
// under "webhook" and constructs via Build (the config-driven path).
func TestWebhookSink_BuildViaRegistry(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := Build("webhook", map[string]any{"url": srv.URL}, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, s)
	// Clean up the background goroutine if it's a WebhookSink.
	if ws, ok := s.(*WebhookSink); ok {
		defer func() { require.NoError(t, ws.Close(context.Background())) }()
	}
}

// TestRegistered_IncludesBuiltin verifies Registered() surfaces all built-in
// sinks (smoke test for the diagnostics helper).
func TestRegistered_IncludesBuiltin(t *testing.T) {
	t.Parallel()
	names := Registered()
	require.Contains(t, names, "noop")
	require.Contains(t, names, "log")
	require.Contains(t, names, "webhook")
}

// TestRegister_NilFactoryPanics verifies the nil-factory guard.
func TestRegister_NilFactoryPanics(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() {
		Register("nil-factory-test", nil)
	})
}
