package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/hrygo/hotplex/internal/config"
)

// GitHubEvent represents the common fields of GitHub webhook payloads.
type GitHubEvent struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Number      int `json:"number"`
	PullRequest *struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	CheckSuite *struct {
		Conclusion   string `json:"conclusion"`
		HeadSHA      string `json:"head_sha"`
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"check_suite"`
	CheckRun *struct {
		Conclusion string `json:"conclusion"`
		CheckSuite *struct {
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"check_suite"`
	} `json:"check_run"`
}

// JobTrigger triggers a named cron job with optional extra context.
type JobTrigger interface {
	TriggerByName(ctx context.Context, jobName string, extra map[string]string) error
}

const maxConcurrentTriggers = 5

// triggerDedupCooldown prevents duplicate triggers for the same PR within this window.
const triggerDedupCooldown = 60 * time.Second

// WebhookHandler handles incoming GitHub webhook requests.
type WebhookHandler struct {
	cfg     config.WebhookConfig
	trigger JobTrigger
	limiter *rate.Limiter
	sem     chan struct{} // bounds concurrent trigger goroutines
	log     *slog.Logger
	baseCtx context.Context    // gateway lifecycle context for async goroutines
	cancel  context.CancelFunc // cancels in-flight triggers on shutdown
	dedup   sync.Map           // pr_number(string) -> time.Time; cooldown-based dedup
}

// NewWebhookHandler creates a new GitHub webhook handler.
// baseCtx should be the gateway lifecycle context so in-flight triggers
// are cancelled on graceful shutdown.
func NewWebhookHandler(baseCtx context.Context, cfg config.WebhookConfig, trigger JobTrigger, log *slog.Logger) *WebhookHandler {
	ctx, cancel := context.WithCancel(baseCtx)
	return &WebhookHandler{
		cfg:     cfg,
		trigger: trigger,
		limiter: rate.NewLimiter(2, 10), // 2 req/s, burst 10
		sem:     make(chan struct{}, maxConcurrentTriggers),
		log:     log.With("component", "webhook"),
		baseCtx: ctx,
		cancel:  cancel,
	}
}

// Close cancels in-flight trigger goroutines and clears dedup entries.
// Safe to call multiple times.
func (h *WebhookHandler) Close() {
	h.cancel()
	h.dedup.Range(func(key, _ any) bool {
		h.dedup.Delete(key)
		return true
	})
}

// ServeHTTP handles incoming GitHub webhook HTTP requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Method check
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. Rate limit
	if !h.limiter.Allow() {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	// 3. Read body with size limit
	body := io.LimitReader(r.Body, h.cfg.MaxBodySize+1)
	payload, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Reject if body exceeds limit (we read one extra byte to detect overflow).
	if int64(len(payload)) > h.cfg.MaxBodySize {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	// 4. HMAC-SHA256 signature verification
	sig := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(payload, sig) {
		h.log.Warn("webhook: invalid signature", "remote", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// 5. Ping event (GitHub sends this on webhook creation)
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		h.log.Info("webhook: ping received", "repo", extractRepoFromPing(payload))
		w.WriteHeader(http.StatusOK)
		return
	}

	// 6. Parse event
	var event GitHubEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		h.log.Warn("webhook: invalid JSON payload", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// 7. Repository filter
	if len(h.cfg.AllowedRepos) > 0 && !slices.Contains(h.cfg.AllowedRepos, event.Repository.FullName) {
		h.log.Warn("webhook: unexpected repo", "repo", event.Repository.FullName)
		w.WriteHeader(http.StatusOK) // 200 to prevent GitHub retries
		return
	}

	// 8. Extract PRs that need review
	prNumbers := h.extractPRs(eventType, &event)
	if len(prNumbers) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 9. Async trigger with dedup and bounded concurrency
	jobName := h.cfg.TargetJobName
	for _, prNum := range prNumbers {
		// Dedup: skip if the same PR was triggered within the cooldown window.
		prKey := strconv.Itoa(prNum)
		if last, ok := h.dedup.Load(prKey); ok {
			if t, _ := last.(time.Time); time.Since(t) < triggerDedupCooldown {
				h.log.Info("webhook: skipping duplicate trigger", "pr", prNum)
				continue
			}
		}
		h.dedup.Store(prKey, time.Now())

		select {
		case h.sem <- struct{}{}:
			go func(n int) {
				defer func() { <-h.sem }()
				ctx, cancel := context.WithTimeout(h.baseCtx, 5*time.Minute)
				defer cancel()
				err := h.trigger.TriggerByName(ctx, jobName, map[string]string{
					"trigger":   "webhook",
					"pr_number": strconv.Itoa(n),
				})
				if err != nil {
					h.log.Error("webhook: trigger failed", "pr", n, "err", err)
				} else {
					h.log.Info("webhook: triggered review", "pr", n)
				}
			}(prNum)
		default:
			h.log.Warn("webhook: trigger concurrency limit reached, skipping", "pr", prNum)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

// verifySignature verifies the HMAC-SHA256 signature in constant time.
func (h *WebhookHandler) verifySignature(payload []byte, sig string) bool {
	if h.cfg.Secret == "" || sig == "" {
		return false
	}
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(sig, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(h.cfg.Secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sigBytes, expected)
}

// extractPRs returns PR numbers that need review based on event type and action.
func (h *WebhookHandler) extractPRs(eventType string, e *GitHubEvent) []int {
	switch eventType {
	case "pull_request":
		if e.PullRequest == nil || e.PullRequest.State != "open" || e.PullRequest.Draft {
			return nil
		}
		switch e.Action {
		case "opened", "synchronize", "reopened", "ready_for_review":
			return []int{e.PullRequest.Number}
		}

	case "check_suite":
		if e.CheckSuite == nil || e.CheckSuite.Conclusion != "success" {
			return nil
		}
		// NOTE: check_suite/check_run PullRequests may include draft or already-merged
		// PRs (GitHub associates suites with commits, not PR state). We intentionally
		// don't filter here — the downstream agent's CI gate will skip irrelevant PRs.
		prs := make([]int, 0, len(e.CheckSuite.PullRequests))
		for _, pr := range e.CheckSuite.PullRequests {
			prs = append(prs, pr.Number)
		}
		return prs

	case "check_run":
		if e.CheckRun == nil || e.CheckRun.Conclusion != "success" || e.CheckRun.CheckSuite == nil {
			return nil
		}
		prs := make([]int, 0, len(e.CheckRun.CheckSuite.PullRequests))
		for _, pr := range e.CheckRun.CheckSuite.PullRequests {
			prs = append(prs, pr.Number)
		}
		return prs
	}
	return nil
}

// extractRepoFromPing attempts to extract the repository full name from a ping payload.
func extractRepoFromPing(payload []byte) string {
	var ping struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(payload, &ping); err == nil {
		return ping.Repository.FullName
	}
	return ""
}
