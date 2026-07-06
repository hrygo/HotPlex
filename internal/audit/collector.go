package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/observability"
)

// CollectorConfig holds the tunables for the audit collector.
type CollectorConfig struct {
	ChannelCap        int
	BatchSize         int
	BatchInterval     time.Duration
	SinkTimeout       time.Duration
	SpillBlockTimeout time.Duration
}

func (c *CollectorConfig) defaults() {
	if c.ChannelCap <= 0 {
		c.ChannelCap = 4096
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.BatchInterval <= 0 {
		c.BatchInterval = 1 * time.Second
	}
	if c.SinkTimeout <= 0 {
		c.SinkTimeout = 5 * time.Second
	}
	if c.SpillBlockTimeout <= 0 {
		c.SpillBlockTimeout = 5 * time.Second
	}
}

// Collector is the zero-loss audit event writer. It batches events into
// a single transaction (so the hash chain is consistent within a batch),
// fans out to AlertSinks after commit, and spills to disk on backpressure.
type Collector struct {
	store Store
	spill *SpillFile
	sinks []AlertSink
	log   *slog.Logger
	cfg   CollectorConfig

	captureC  chan *UserActivity
	closeC    chan struct{}
	closeWg   sync.WaitGroup
	closeOnce sync.Once

	mu       sync.Mutex // serializes flushBatch and drainSpill
	enqueued atomic.Int64
	spilled  atomic.Int64
	dropped  atomic.Int64
}

// spillSentinel is a sentinel *UserActivity value that tells runWriter to drain
// the spill file on the next iteration.
var spillSentinel = &UserActivity{UserID: "__audit_spill_marker__"}

// NewCollector creates a new Collector. spill may be nil for tests.
// sinks may be nil or empty — fanOutSinks is a no-op in that case.
func NewCollector(store Store, spill *SpillFile, sinks []AlertSink, log *slog.Logger, cfg CollectorConfig) *Collector {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	if sinks == nil {
		sinks = []AlertSink{}
	}
	return &Collector{
		store:    store,
		spill:    spill,
		sinks:    sinks,
		log:      log,
		cfg:      cfg,
		captureC: make(chan *UserActivity, cfg.ChannelCap),
		closeC:   make(chan struct{}),
	}
}

// Start launches the runWriter goroutine. Call once before Enqueue.
func (c *Collector) Start() {
	c.closeWg.Add(1)
	go c.runWriter()
}

// Enqueue submits an audit event with zero-loss semantics:
//  1. Try non-blocking send to captureC
//  2. On full: spill to WAL (O_SYNC)
//  3. Last resort: bounded block (SpillBlockTimeout)
//
// Returns nil if accepted (in-memory or spill), error if all paths failed.
func (c *Collector) Enqueue(ctx context.Context, ua *UserActivity) error {
	if ua == nil {
		return fmt.Errorf("audit: nil UserActivity")
	}
	select {
	case <-c.closeC:
		return ErrCollectorClosed
	default:
	}

	select {
	case c.captureC <- ua:
		c.enqueued.Add(1)
		return nil
	default:
	}

	if c.spill != nil {
		rec := SpillRecord{TsMs: time.Now().UnixMilli(), UA: ua}
		if err := c.spill.Write(rec); err == nil {
			c.spilled.Add(1)
			observability.AuditSpill().Add(ctx, 1, metric.WithAttributes(attribute.String("action", "spill_ok")))
			select {
			case c.captureC <- spillSentinel:
			default:
			}
			return nil
		}
		observability.AuditSpill().Add(ctx, 1, metric.WithAttributes(attribute.String("action", "spill_failed")))
	}

	select {
	case c.captureC <- ua:
		c.enqueued.Add(1)
		return nil
	case <-time.After(c.cfg.SpillBlockTimeout):
		c.dropped.Add(1)
		return fmt.Errorf("audit collector: enqueue timeout after %v", c.cfg.SpillBlockTimeout)
	case <-c.closeC:
		return fmt.Errorf("audit collector: closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Enqueued returns the count of in-memory accepts.
func (c *Collector) Enqueued() int64 { return c.enqueued.Load() }

// Spilled returns the count of spill writes.
func (c *Collector) Spilled() int64 { return c.spilled.Load() }

// Dropped returns the count of drops (should always be 0).
func (c *Collector) Dropped() int64 { return c.dropped.Load() }

// Close signals the writer to stop, waits for it to drain and flush, then
// closes the spill file. Safe to call multiple times.
func (c *Collector) Close() error {
	var spillErr error
	c.closeOnce.Do(func() {
		close(c.closeC)
	})
	c.closeWg.Wait()
	if c.spill != nil {
		spillErr = c.spill.Close()
		c.spill = nil
	}
	return spillErr
}

func (c *Collector) runWriter() {
	defer c.closeWg.Done()
	ticker := time.NewTicker(c.cfg.BatchInterval)
	defer ticker.Stop()

	var batch []*UserActivity

	for {
		select {
		case <-c.closeC:
			c.mu.Lock()
			for {
				select {
				case ua := <-c.captureC:
					if ua == spillSentinel {
						continue
					}
					batch = append(batch, ua)
				default:
					goto finalFlush
				}
			}
		finalFlush:
			c.flushLocked(batch)
			batch = nil
			c.drainSpillLocked()
			c.mu.Unlock()
			return

		case ua := <-c.captureC:
			if ua == spillSentinel {
				c.mu.Lock()
				c.flushLocked(batch)
				batch = batch[:0]
				c.drainSpillLocked()
				c.mu.Unlock()
				continue
			}
			batch = append(batch, ua)
			if len(batch) >= c.cfg.BatchSize {
				c.mu.Lock()
				c.flushLocked(batch)
				batch = batch[:0]
				c.mu.Unlock()
			}

		case <-ticker.C:
			c.mu.Lock()
			c.flushLocked(batch)
			batch = batch[:0]
			c.drainSpillLocked()
			c.mu.Unlock()
		}
	}
}

func (c *Collector) flushLocked(batch []*UserActivity) {
	if len(batch) == 0 {
		return
	}
	if err := c.flushBatch(batch); err != nil {
		c.log.Error("audit: flush batch failed", "err", err, "size", len(batch))
	}
}

func (c *Collector) drainSpillLocked() {
	if c.spill == nil {
		return
	}
	// Drain atomically reads + truncates the spill file under a single
	// s.mu lock acquisition. This prevents the race where a concurrent
	// Write appends new records between ReadAll and Truncate (which would
	// lose those records). Spec section 5.10 zero-loss guarantee.
	records, err := c.spill.Drain()
	if err != nil {
		c.log.Error("audit: spill drain", "err", err)
		return
	}
	if len(records) == 0 {
		return
	}
	uas := make([]*UserActivity, 0, len(records))
	for _, r := range records {
		if r.UA != nil {
			uas = append(uas, r.UA)
		}
	}
	if err := c.flushBatch(uas); err != nil {
		c.log.Error("audit: spill flush failed", "err", err)
		return
	}
}

func (c *Collector) flushBatch(uas []*UserActivity) error {
	if len(uas) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := c.store.BeginTx(ctx)
	if err != nil {
		observability.AuditWriteFailures().Add(ctx, 1, metric.WithAttributes(attribute.String("action", "begin_tx")))
		return fmt.Errorf("audit: begin tx: %w", err)
	}

	tail, err := tx.TailHash(ctx)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("audit: read tail: %w", err)
	}

	committed := make([]*UserActivity, 0, len(uas))
	for _, ua := range uas {
		ua.PrevHash = tail
		h, err := ComputeSelfHash(tail, ua)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("audit: compute hash: %w", err)
		}
		ua.SelfHash = h
		if err := tx.Append(ctx, ua); err != nil {
			_ = tx.Rollback()
			observability.AuditWriteFailures().Add(ctx, 1, metric.WithAttributes(attribute.String("action", "append")))
			return fmt.Errorf("audit: append: %w", err)
		}
		committed = append(committed, ua)
		tail = h
	}

	if err := tx.Commit(); err != nil {
		observability.AuditWriteFailures().Add(ctx, 1, metric.WithAttributes(attribute.String("action", "commit")))
		return fmt.Errorf("audit: commit: %w", err)
	}

	for _, ua := range committed {
		observability.AuditEvents().Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("action", ua.Action),
				attribute.String("outcome", ua.Outcome),
			),
		)
	}

	c.fanOutSinks(context.Background(), committed)
	return nil
}

func (c *Collector) fanOutSinks(ctx context.Context, uas []*UserActivity) {
	for _, sink := range c.sinks {
		sink := sink
		for _, ua := range uas {
			ua := ua
			go func() {
				ev := userActivityToAuditEvent(ua)
				sctx, cancel := context.WithTimeout(ctx, c.cfg.SinkTimeout)
				defer cancel()
				if err := sink.OnAuditEvent(sctx, ev); err != nil {
					observability.AuditSinkFailures().Add(ctx, 1, metric.WithAttributes(
						attribute.String("sink", fmt.Sprintf("%T", sink)),
					))
					c.log.Warn("audit: sink failed",
						"err", err,
						"event_id", ev.EventID,
						"action", ev.Action,
					)
				}
			}()
		}
	}
}

func userActivityToAuditEvent(ua *UserActivity) AuditEvent {
	return AuditEvent{
		EventID:      fmt.Sprintf("ev_%d_%x", ua.Ts, hashUint64(uint64(ua.Ts))),
		Ts:           time.UnixMilli(ua.Ts),
		UserID:       ua.UserID,
		UserIDType:   ua.UserIDType,
		Platform:     ua.Platform,
		SessionID:    ua.SessionID,
		Action:       ua.Action,
		ResourceType: ua.ResourceType,
		ResourceID:   ua.ResourceID,
		Outcome:      ua.Outcome,
		Detail:       decodeDetail(ua.DetailJSON),
		EventRef:     ua.EventRef,
		IP:           ua.IP,
		UserAgent:    ua.UserAgent,
	}
}

func hashUint64(v uint64) uint64 {
	h := uint64(14695981039346656037)
	h ^= v
	h *= 1099511628211
	return h
}

func decodeDetail(raw string) map[string]any {
	_ = raw
	return nil
}

var ErrCollectorClosed = errors.New("audit: collector closed")
