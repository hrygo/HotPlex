package sinks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// WebhookSink POSTs each audit event as JSON to a configured URL, signed with
// HMAC-SHA256 so receivers can verify authenticity. It is the reference
// implementation of the external RegisterSink contract (spec §5.6, issue #833
// P2) and the primary built-in sink for SIEM/SOC integration.
//
// Configuration (audit.sinks[].config):
//
//	url (required) — HTTPS endpoint receiving POST /application/json.
//	secret (optional) — HMAC-SHA256 key; when set, each request carries an
//	  X-Audit-Signature header = hex(hmac-sha256(body, secret)). Receivers
//	  SHOULD reject unsigned requests when a secret is configured.
//	timeout (optional, default 5s) — per-POST HTTP timeout.
//	queue_cap (optional, default 1024) — bounded internal queue; overflow
//	  drops events + increments Failures() and hotplex.audit.sink_failures.
//
// Delivery guarantees: at-most-once per enqueue. Retries are best-effort
// (3 attempts, exponential backoff 1s/2s/4s); a permanently failed event is
// dropped after retries to keep the queue draining. The audit row was already
// persisted to the tamper-evident table before the sink sees it, so a dropped
// webhook delivery does not lose audit data.
type WebhookSink struct {
	client    *http.Client
	url       string
	secret    string
	queueCap  int
	queue     chan AlertEvent
	failures  atomic.Int64
	delivered atomic.Int64
	log       *slog.Logger
	cancel    context.CancelFunc
	done      chan struct{}
}

// webhookConfig keys.
const (
	webhookDefaultTimeout  = 5 * time.Second
	webhookDefaultQueueCap = 1024
	webhookRetryAttempts   = 3
	webhookRetryBaseDelay  = 1 * time.Second
)

// NewWebhookSink constructs a WebhookSink from a config map. It launches a
// background delivery goroutine that lives until Close. The factory is
// pre-registered as "webhook" in registry.go init().
func NewWebhookSink(cfg map[string]any, log *slog.Logger) (*WebhookSink, error) {
	if log == nil {
		log = slog.Default()
	}
	url, _ := cfg["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("audit webhook sink: config 'url' is required")
	}
	secret, _ := cfg["secret"].(string)
	timeout := webhookDefaultTimeout
	if v, ok := cfg["timeout"]; ok {
		if d, err := parseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	queueCap := webhookDefaultQueueCap
	if v, ok := cfg["queue_cap"]; ok {
		if n, err := parseInt(v); err == nil && n > 0 {
			queueCap = n
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &WebhookSink{
		client:   &http.Client{Timeout: timeout},
		url:      url,
		secret:   secret,
		queueCap: queueCap,
		queue:    make(chan AlertEvent, queueCap),
		log:      log,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go s.run(ctx)
	return s, nil
}

// OnAlertEvent enqueues an event for async delivery. It NEVER blocks for longer
// than it takes to push onto a buffered channel — when the queue is full the
// event is dropped and Failures() increments (spec §5.6 backpressure).
func (s *WebhookSink) OnAlertEvent(_ context.Context, e AlertEvent) error {
	select {
	case s.queue <- e:
	default:
		s.failures.Add(1)
	}
	return nil
}

// Failures returns the count of dropped/permanently-failed events.
func (s *WebhookSink) Failures() int64 { return s.failures.Load() }

// Delivered returns the count of successfully POSTed events.
func (s *WebhookSink) Delivered() int64 { return s.delivered.Load() }

// Close drains pending events (best-effort) and stops the delivery goroutine.
func (s *WebhookSink) Close() {
	s.cancel()
	<-s.done
}

// run is the single delivery goroutine. Events are delivered sequentially to
// preserve order; burst handling relies on the queue buffer, not parallelism
// (ordering matters for audit chains).
func (s *WebhookSink) run(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-s.queue:
			s.deliverWithRetry(ctx, e)
		}
	}
}

// deliverWithRetry attempts delivery with exponential backoff. A final failure
// increments Failures() and is logged — the event is not re-enqueued (would
// risk unbounded growth behind a sustained outage).
func (s *WebhookSink) deliverWithRetry(ctx context.Context, e AlertEvent) {
	body, err := json.Marshal(e)
	if err != nil {
		s.failures.Add(1)
		s.log.Warn("audit webhook: marshal failed", "err", err, "event_id", e.EventID)
		return
	}
	delay := webhookRetryBaseDelay
	for attempt := 1; attempt <= webhookRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			s.failures.Add(1)
			return
		}
		if err := s.post(ctx, body); err == nil {
			s.delivered.Add(1)
			return
		} else if attempt < webhookRetryAttempts {
			s.log.Debug("audit webhook: transient failure, retrying",
				"attempt", attempt, "err", err, "next_delay", delay)
			select {
			case <-ctx.Done():
				s.failures.Add(1)
				return
			case <-time.After(delay):
			}
			delay *= 2
		} else {
			s.failures.Add(1)
			s.log.Warn("audit webhook: delivery failed after retries, dropping",
				"attempts", attempt, "err", err, "event_id", e.EventID)
		}
	}
}

// post performs a single signed POST. Returns nil on 2xx, error otherwise.
func (s *WebhookSink) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Audit-Event-Source", "hotplex")
	if s.secret != "" {
		mac := hmac.New(sha256.New, []byte(s.secret))
		mac.Write(body)
		req.Header.Set("X-Audit-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// parseDuration accepts string (Go duration syntax) or numeric (seconds) forms
// from the loosely-typed config map.
func parseDuration(v any) (time.Duration, error) {
	switch x := v.(type) {
	case string:
		return time.ParseDuration(x)
	case float64:
		return time.Duration(x) * time.Second, nil
	case int:
		return time.Duration(x) * time.Second, nil
	case int64:
		return time.Duration(x) * time.Second, nil
	}
	return 0, fmt.Errorf("unsupported duration type %T", v)
}

// parseInt accepts numeric or string-numeric forms.
func parseInt(v any) (int, error) {
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case string:
		var n int
		_, err := fmt.Sscanf(x, "%d", &n)
		return n, err
	}
	return 0, fmt.Errorf("unsupported int type %T", v)
}

func init() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry["webhook"] = func(cfg map[string]any, log *slog.Logger) (Sink, error) {
		return NewWebhookSink(cfg, log)
	}
}
