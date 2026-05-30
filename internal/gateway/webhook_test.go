package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const testSecret = "test-webhook-secret"

// prDetail is a typed alias for the PullRequest inner struct to avoid repetition.
type prDetail = struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// checkSuiteDetail is a typed alias for the CheckSuite inner struct.
type checkSuiteDetail = struct {
	Conclusion   string `json:"conclusion"`
	HeadSHA      string `json:"head_sha"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postWebhook(h *WebhookHandler, eventType string, payload any) *httptest.ResponseRecorder {
	var body []byte
	switch p := payload.(type) {
	case []byte:
		body = p
	default:
		var err error
		body, err = json.Marshal(p)
		if err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-Hub-Signature-256", signPayload(testSecret, body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func newIsolatedHandler() (*WebhookHandler, *mockTrigger) {
	t := &mockTrigger{}
	h := NewWebhookHandler(WebhookConfig{
		Enabled:     true,
		Secret:      testSecret,
		Path:        "/api/webhook/github",
		MaxBodySize: 1 << 20,
	}, t, noopLogger())
	return h, t
}

// --- Signature verification unit tests (AC-3) ---

func TestWebhookHandler_SignatureVerification(t *testing.T) {
	t.Parallel()

	h := &WebhookHandler{
		cfg: WebhookConfig{Secret: testSecret, MaxBodySize: 1 << 20},
	}

	payload := []byte(`{"action":"opened"}`)

	t.Run("valid signature", func(t *testing.T) {
		t.Parallel()
		sig := signPayload(testSecret, payload)
		ok := h.verifySignature(payload, sig)
		require.True(t, ok)
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		ok := h.verifySignature(payload, "sha256=deadbeef")
		require.False(t, ok)
	})

	t.Run("missing signature", func(t *testing.T) {
		t.Parallel()
		ok := h.verifySignature(payload, "")
		require.False(t, ok)
	})

	t.Run("empty secret rejects all", func(t *testing.T) {
		t.Parallel()
		empty := &WebhookHandler{cfg: WebhookConfig{Secret: "", MaxBodySize: 1 << 20}}
		sig := signPayload(testSecret, payload)
		ok := empty.verifySignature(payload, sig)
		require.False(t, ok)
	})

	t.Run("wrong prefix", func(t *testing.T) {
		t.Parallel()
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write(payload)
		ok := h.verifySignature(payload, "sha1="+hex.EncodeToString(mac.Sum(nil)))
		require.False(t, ok)
	})

	t.Run("malformed hex", func(t *testing.T) {
		t.Parallel()
		ok := h.verifySignature(payload, "sha256=ZZZZ")
		require.False(t, ok)
	})
}

// --- Event filtering unit tests (AC-4) ---

func TestWebhookHandler_ExtractPRs(t *testing.T) {
	t.Parallel()

	h := &WebhookHandler{cfg: WebhookConfig{MaxBodySize: 1 << 20}}

	t.Run("draft PR ignored", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "opened",
			PullRequest: &prDetail{Number: 42, State: "open", Draft: true},
		}
		prs := h.extractPRs("pull_request", e)
		require.Empty(t, prs)
	})

	t.Run("closed PR ignored", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "closed",
			PullRequest: &prDetail{Number: 42, State: "closed"},
		}
		prs := h.extractPRs("pull_request", e)
		require.Empty(t, prs)
	})

	t.Run("opened triggers review", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "opened",
			PullRequest: &prDetail{Number: 42, State: "open"},
		}
		prs := h.extractPRs("pull_request", e)
		require.Equal(t, []int{42}, prs)
	})

	t.Run("synchronize triggers review", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "synchronize",
			PullRequest: &prDetail{Number: 42, State: "open"},
		}
		prs := h.extractPRs("pull_request", e)
		require.Equal(t, []int{42}, prs)
	})

	t.Run("ready_for_review triggers", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "ready_for_review",
			PullRequest: &prDetail{Number: 42, State: "open"},
		}
		prs := h.extractPRs("pull_request", e)
		require.Equal(t, []int{42}, prs)
	})

	t.Run("check_suite success triggers PRs", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			CheckSuite: &checkSuiteDetail{
				Conclusion: "success",
				PullRequests: []struct {
					Number int `json:"number"`
				}{{Number: 42}},
			},
		}
		prs := h.extractPRs("check_suite", e)
		require.Equal(t, []int{42}, prs)
	})

	t.Run("check_suite failure ignored", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			CheckSuite: &checkSuiteDetail{
				Conclusion: "failure",
				PullRequests: []struct {
					Number int `json:"number"`
				}{{Number: 42}},
			},
		}
		prs := h.extractPRs("check_suite", e)
		require.Empty(t, prs)
	})

	t.Run("unknown event type returns nil", func(t *testing.T) {
		t.Parallel()
		prs := h.extractPRs("push", &GitHubEvent{})
		require.Empty(t, prs)
	})

	t.Run("unknown PR action returns nil", func(t *testing.T) {
		t.Parallel()
		e := &GitHubEvent{
			Action:      "labeled",
			PullRequest: &prDetail{Number: 42, State: "open"},
		}
		prs := h.extractPRs("pull_request", e)
		require.Empty(t, prs)
	})
}

// --- ServeHTTP integration tests ---

type mockTrigger struct {
	triggered atomic.Int32
	mu        sync.Mutex
	lastExtra map[string]string
}

func (m *mockTrigger) TriggerByName(_ context.Context, _ string, extra map[string]string) error {
	m.triggered.Add(1)
	m.mu.Lock()
	m.lastExtra = extra
	m.mu.Unlock()
	return nil
}

func (m *mockTrigger) count() int { return int(m.triggered.Load()) }

func (m *mockTrigger) getLastExtra() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastExtra
}

func TestWebhookHandler_ServeHTTP(t *testing.T) {
	t.Parallel()

	t.Run("GET returns 405", func(t *testing.T) {
		t.Parallel()
		h, _ := newIsolatedHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/webhook/github", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("missing signature returns 403", func(t *testing.T) {
		t.Parallel()
		h, _ := newIsolatedHandler()
		req := httptest.NewRequest(http.MethodPost, "/api/webhook/github", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid signature returns 403", func(t *testing.T) {
		t.Parallel()
		h, _ := newIsolatedHandler()
		req := httptest.NewRequest(http.MethodPost, "/api/webhook/github", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("ping returns 200 without trigger", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "ping", []byte(`{"zen":"Keep it logical.","repository":{"full_name":"hrygo/hotplex"}}`))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, 0, trigger.count())
	})

	t.Run("wrong repo returns 200 without trigger", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "pull_request", &GitHubEvent{
			Action: "opened",
			Repository: struct {
				FullName string `json:"full_name"`
			}{FullName: "other/repo"},
		})
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, 0, trigger.count())
	})

	t.Run("valid PR opened triggers review", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "pull_request", &GitHubEvent{
			Action: "opened",
			Repository: struct {
				FullName string `json:"full_name"`
			}{
				FullName: "hrygo/hotplex",
			},
			PullRequest: &prDetail{Number: 99, State: "open"},
		})
		require.Equal(t, http.StatusAccepted, w.Code)

		require.Eventually(t, func() bool { return trigger.count() >= 1 }, 2*time.Second, 50*time.Millisecond)
		require.Equal(t, "webhook", trigger.getLastExtra()["trigger"])
		require.Equal(t, "99", trigger.getLastExtra()["pr_number"])
	})

	t.Run("draft PR does not trigger", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "pull_request", &GitHubEvent{
			Action: "opened",
			Repository: struct {
				FullName string `json:"full_name"`
			}{
				FullName: "hrygo/hotplex",
			},
			PullRequest: &prDetail{Number: 50, State: "open", Draft: true},
		})
		require.Equal(t, http.StatusOK, w.Code) // no PRs extracted → 200
		require.Equal(t, 0, trigger.count())
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		t.Parallel()
		h, _ := newIsolatedHandler()
		w := postWebhook(h, "pull_request", []byte(`{invalid json`))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("oversized payload returns 413", func(t *testing.T) {
		t.Parallel()
		h, _ := newIsolatedHandler()
		bigPayload := make([]byte, 1<<20+100) // > 1MB
		for i := range bigPayload {
			bigPayload[i] = 'a'
		}
		w := postWebhook(h, "pull_request", bigPayload)
		require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})

	t.Run("check_suite success triggers review", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "check_suite", &GitHubEvent{
			Action: "completed",
			Repository: struct {
				FullName string `json:"full_name"`
			}{
				FullName: "hrygo/hotplex",
			},
			CheckSuite: &checkSuiteDetail{
				Conclusion: "success",
				HeadSHA:    "abc123",
				PullRequests: []struct {
					Number int `json:"number"`
				}{{Number: 55}},
			},
		})
		require.Equal(t, http.StatusAccepted, w.Code)
		require.Eventually(t, func() bool { return trigger.count() >= 1 }, 2*time.Second, 50*time.Millisecond)
		require.Equal(t, "55", trigger.getLastExtra()["pr_number"])
	})

	t.Run("check_suite failure does not trigger", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "check_suite", &GitHubEvent{
			Action: "completed",
			Repository: struct {
				FullName string `json:"full_name"`
			}{
				FullName: "hrygo/hotplex",
			},
			CheckSuite: &checkSuiteDetail{
				Conclusion: "failure",
				HeadSHA:    "abc123",
				PullRequests: []struct {
					Number int `json:"number"`
				}{{Number: 55}},
			},
		})
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, 0, trigger.count())
	})

	t.Run("unknown event returns 200 without trigger", func(t *testing.T) {
		t.Parallel()
		h, trigger := newIsolatedHandler()
		w := postWebhook(h, "push", &GitHubEvent{
			Repository: struct {
				FullName string `json:"full_name"`
			}{
				FullName: "hrygo/hotplex",
			},
		})
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, 0, trigger.count())
	})
}

// --- Rate limiting tests (AC-6) ---

func TestWebhookHandler_RateLimiting(t *testing.T) {
	t.Parallel()

	h, _ := newIsolatedHandler()

	// Exhaust burst (10 tokens)
	var limited int
	for range 20 {
		payload := []byte(`{}`)
		req := httptest.NewRequest(http.MethodPost, "/api/webhook/github", bytes.NewReader(payload))
		req.Header.Set("X-Hub-Signature-256", "sha256=abc")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	require.Greater(t, limited, 0, "some requests should be rate limited after burst exhaustion")
}

// --- Trigger error (AC-5.3) ---

type errorTrigger struct{}

func (e *errorTrigger) TriggerByName(_ context.Context, _ string, _ map[string]string) error {
	return fmt.Errorf("job not found: pr-review-hotplex")
}

func TestWebhookHandler_TriggerError(t *testing.T) {
	t.Parallel()

	h := NewWebhookHandler(WebhookConfig{
		Enabled:     true,
		Secret:      testSecret,
		Path:        "/api/webhook/github",
		MaxBodySize: 1 << 20,
	}, &errorTrigger{}, noopLogger())

	w := postWebhook(h, "pull_request", &GitHubEvent{
		Action: "opened",
		Repository: struct {
			FullName string `json:"full_name"`
		}{
			FullName: "hrygo/hotplex",
		},
		PullRequest: &prDetail{Number: 1, State: "open"},
	})
	// HTTP response is 202 even if trigger fails (async)
	require.Equal(t, http.StatusAccepted, w.Code)
}

// --- Empty secret (AC-3.5) ---

func TestWebhookHandler_EmptySecret(t *testing.T) {
	t.Parallel()

	h, _ := newIsolatedHandler()
	h.cfg.Secret = ""

	payload := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/github", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signPayload("anything", payload))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}
