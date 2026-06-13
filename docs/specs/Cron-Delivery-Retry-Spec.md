# Cron Delivery Retry Spec

**Issue**: cron delivery retry (follow-up to #574 audit)
**Priority**: P2
**Status**: Implemented
**Author**: Claude Code Audit
**Date**: 2026-05-30

---

## Background

Cron job execution results are delivered to messaging platforms (Slack/Feishu) via `delivery.go`. When delivery fails (network error, API rate limit, invalid channel), the result is **permanently lost** — there is no retry mechanism. The current code logs `log.Error("result permanently lost, no retry mechanism")` but does not attempt re-delivery.

## Problem Statement

A successfully executed cron job whose result cannot be delivered is indistinguishable from a job that never ran. This violates user expectations: if the job executed, the user should eventually see the result.

### Failure Scenarios

1. **Transient network error** — gateway ↔ Slack/Feishu API connection drops
2. **Rate limit (429)** — too many concurrent deliveries
3. **Channel not found** — channel was deleted after job creation
4. **Token expiry** — bot token revoked between job creation and execution

Only scenarios 1–2 are retriable. Scenarios 3–4 are permanent failures.

## Design

### Delivery Queue

Add an in-memory delivery queue to the `Delivery` struct:

```
Delivery
  ├─ mu sync.Mutex
  ├─ queue []pendingDelivery    // FIFO queue
  ├─ semaphore chan struct{}    // limit concurrent retries
  └─ stop chan struct{}         // shutdown signal

pendingDelivery
  ├─ job     *CronJob
  ├─ result  string
  ├─ attempt int
  └─ nextAt  time.Time
```

### Retry Policy

| Parameter | Value | Rationale |
|---|---|---|
| Max attempts | 3 | Balance reliability vs. resource usage |
| Initial backoff | 30s | Allow transient issues to resolve |
| Backoff multiplier | 2x | Exponential: 30s → 1m → 2m |
| Max backoff | 5m | Don't queue stale results forever |
| Max queue size | 100 | Prevent unbounded memory growth |

### Flow

```
deliverResult(job, result)
  ├─ 1st attempt: send to platform
  ├─ success → done
  └─ failure → classify error
       ├─ retriable (429, timeout, network) → enqueue with backoff
       └─ permanent (404, 403) → log + discard
```

### Retrier Goroutine

A single background goroutine processes the queue:

```go
func (d *Delivery) retryLoop(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            d.flushPending(ctx)
        }
    }
}
```

`flushPending` drains all entries where `nextAt <= now`, respects the semaphore for concurrency, and re-enqueues failures with incremented attempt count.

### Lifecycle

- **Start**: `retryLoop` goroutine launched in `Scheduler.Start`
- **Shutdown**: cancel ctx → `retryLoop` exits → remaining items logged as permanently lost
- **Metrics**: `cron_delivery_retry_total{status="success|exhausted|permanent"}` counters

### Persistent Failures

After 3 failed attempts, log with:
- `log.Error("delivery exhausted all retries")`
- Fields: `job_id`, `job_name`, `attempt_count`, `last_error`
- Metric: `cron_delivery_retry_total{status="exhausted"}`

### Non-Goals

- **Persistent retry queue** — in-memory only; gateway restart discards pending retries
- **Dead letter queue** — no secondary storage for failed deliveries
- **Cross-gateway delivery** — single-instance only

## Implementation Plan

1. Add `pendingDelivery` struct and retry queue to `delivery.go`
2. Implement `enqueue`, `flushPending`, and `retryLoop`
3. Update `deliverResult` to call `enqueue` on retriable failures
4. Add retry metrics
5. Update `Scheduler.Start` / `Shutdown` to manage retry goroutine lifecycle
6. Add tests for retry logic (backoff, max attempts, permanent failure detection)

## Acceptance Criteria

- [ ] Transient delivery failures (429, timeout) are retried up to 3 times with exponential backoff
- [ ] Permanent failures (404, 403) are logged and discarded immediately
- [ ] Retry queue is bounded to 100 entries; oldest discarded on overflow
- [ ] Shutdown logs remaining pending deliveries as permanently lost
- [ ] Metrics track retry success/exhaustion/permanent-failure counts
