package cron

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/observability"
)

// ResponseExtractor extracts the last assistant response from a completed session.
type ResponseExtractor func(ctx context.Context, sessionID string) (string, error)

// PlatformDeliverer sends a cron result to a specific platform target.
type PlatformDeliverer func(ctx context.Context, platform string, platformKey map[string]string, response string) error

// Delivery retry constants.
const (
	maxRetryAttempts  = 3
	initialBackoff    = 30 * time.Second
	backoffMultiplier = 2
	maxBackoff        = 5 * time.Minute
	maxQueueSize      = 100
	retryTickInterval = 10 * time.Second
)

// pendingDelivery represents a queued delivery awaiting retry.
type pendingDelivery struct {
	job     *CronJob
	result  string
	attempt int
	nextAt  time.Time
}

// Delivery routes cron job execution results to the originating platform.
type Delivery struct {
	mu        sync.Mutex
	log       *slog.Logger
	extract   ResponseExtractor
	deliverFn PlatformDeliverer

	// queue is an in-memory FIFO retry queue. Entries are lost on process restart.
	queue []pendingDelivery
	wg    sync.WaitGroup
	stop  chan struct{}
}

// NewDelivery creates a new Delivery instance.
func NewDelivery(log *slog.Logger, extract ResponseExtractor, deliverFn PlatformDeliverer) *Delivery {
	return &Delivery{
		log:       log.With("component", "cron_delivery"),
		extract:   extract,
		deliverFn: deliverFn,
	}
}

// Deliver extracts the last response from the session and routes it to the platform.
func (d *Delivery) Deliver(ctx context.Context, job *CronJob, sessionKey string) {
	if d.extract == nil {
		return
	}

	response, err := d.extract(ctx, sessionKey)
	if err != nil {
		d.log.Warn("cron delivery: extract response failed", "job_id", job.ID, "err", err)
		return
	}
	if response == "" {
		return
	}

	// No platform or self-originated = no delivery.
	if job.Platform == "" || job.Platform == "cron" {
		return
	}

	d.deliverResult(ctx, job, response, 1)
}

// deliverResult attempts delivery and enqueues for retry on retriable failures.
func (d *Delivery) deliverResult(ctx context.Context, job *CronJob, response string, attempt int) {
	d.mu.Lock()
	fn := d.deliverFn
	d.mu.Unlock()

	if fn == nil {
		d.log.Debug("cron delivery: no platform deliverer configured", "platform", job.Platform)
		return
	}

	if err := fn(ctx, job.Platform, job.PlatformKey, response); err != nil {
		if isTemporaryError(err) && attempt < maxRetryAttempts {
			backoff := retryBackoff(attempt)
			d.log.Warn("cron delivery: transient failure, enqueuing for retry",
				"job_id", job.ID, "name", job.Name, "attempt", attempt,
				"next_attempt", attempt+1, "backoff", backoff, "err", err)
			d.enqueue(job, response, attempt+1, time.Now().Add(backoff))
			return
		}

		status := "permanent"
		if isTemporaryError(err) {
			status = "exhausted"
		}
		// Use caller ctx to preserve trace linkage; OTel synchronous
		// counters record regardless of ctx cancellation.
		observability.CronDeliveryRetry().Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("status", status),
				attribute.String("platform", job.Platform),
			))

		d.log.Error("cron delivery: deliver failed — result permanently lost",
			"job_id", job.ID, "name", job.Name, "platform", job.Platform,
			"response_len", len(response), "attempt", attempt, "err", err)
		return
	}

	// Record success on retry (attempt > 1 means this was a retry).
	if attempt > 1 {
		observability.CronDeliveryRetry().Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("status", "success"),
				attribute.String("platform", job.Platform),
			))
		d.log.Info("cron delivery: retry succeeded",
			"job_id", job.ID, "name", job.Name, "attempt", attempt, "platform", job.Platform)
	}
}

// enqueue adds a pending delivery to the retry queue.
// Evicts the oldest entry if the queue is full.
func (d *Delivery) enqueue(job *CronJob, result string, attempt int, nextAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.queue) >= maxQueueSize {
		evicted := d.queue[0]
		d.queue = d.queue[1:]
		d.log.Warn("cron delivery: retry queue full, evicting oldest entry",
			"evicted_job_id", evicted.job.ID, "evicted_name", evicted.job.Name)
	}

	d.queue = append(d.queue, pendingDelivery{
		job:     job,
		result:  result,
		attempt: attempt,
		nextAt:  nextAt,
	})
}

// StartRetryLoop launches the background retry goroutine.
// Idempotent: safe to call multiple times; only the first call starts the loop.
func (d *Delivery) StartRetryLoop(ctx context.Context) {
	d.mu.Lock()
	if d.stop != nil {
		// Already started.
		d.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	d.stop = stop
	d.wg.Add(1)
	d.mu.Unlock()

	go d.retryLoop(ctx, stop)
}

// StopRetryLoop signals the retry goroutine to stop and waits for it to drain.
// Idempotent: safe to call multiple times; only the first call signals stop and drains.
func (d *Delivery) StopRetryLoop() {
	stopped := false
	d.mu.Lock()
	if d.stop != nil {
		close(d.stop)
		d.stop = nil
		stopped = true
	}
	d.mu.Unlock()

	if !stopped {
		return
	}

	d.wg.Wait()

	// Log remaining pending deliveries as permanently lost.
	d.mu.Lock()
	remaining := d.queue
	d.queue = nil
	d.mu.Unlock()

	for _, pd := range remaining {
		d.log.Error("cron delivery: shutdown with pending retry, result permanently lost",
			"job_id", pd.job.ID, "name", pd.job.Name, "attempt", pd.attempt, "platform", pd.job.Platform)
	}
}

// retryLoop periodically drains the retry queue.
// stop is captured at goroutine start to avoid reading d.stop field
// after StopRetryLoop nils it.
func (d *Delivery) retryLoop(ctx context.Context, stop chan struct{}) {
	defer d.wg.Done()

	ticker := time.NewTicker(retryTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			d.flushPending(ctx)
		}
	}
}

// flushPending drains all due entries from the retry queue and re-attempts delivery.
func (d *Delivery) flushPending(ctx context.Context) {
	now := time.Now()
	var due []pendingDelivery

	d.mu.Lock()
	var remaining []pendingDelivery
	for _, pd := range d.queue {
		if pd.nextAt.Compare(now) <= 0 {
			due = append(due, pd)
		} else {
			remaining = append(remaining, pd)
		}
	}
	d.queue = remaining
	d.mu.Unlock()

	for _, pd := range due {
		d.deliverResult(ctx, pd.job, pd.result, pd.attempt)
	}
}

// retryBackoff computes the backoff duration for a given attempt number.
// Uses exponential backoff: 30s, 1m, 2m, capped at 5m.
// Unlike retry.backoff (table-based, for job execution retry up to 1h),
// this is purpose-built for delivery retry with a shorter cap.
func retryBackoff(attempt int) time.Duration {
	d := initialBackoff
	for i := 1; i < attempt; i++ {
		d *= backoffMultiplier
		if d > maxBackoff {
			return maxBackoff
		}
	}
	return d
}

// SetDeliverer sets the platform deliverer function after construction.
// Used when the deliverer depends on adapters initialized later than the cron scheduler.
func (d *Delivery) SetDeliverer(fn PlatformDeliverer) {
	d.mu.Lock()
	d.deliverFn = fn
	d.mu.Unlock()
}
