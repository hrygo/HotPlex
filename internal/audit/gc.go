// Package audit — gc.go implements retention garbage collection with checkpoint rebase.
// See spec §5.5 (checkpoint rebase) and §5.7 (retention GC).
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// GCConfig holds GC tunables.
type GCConfig struct {
	Retention time.Duration // default 3 years
	Interval  time.Duration // default 1 hour
}

func (c *GCConfig) defaults() {
	if c.Retention <= 0 {
		c.Retention = 3 * 365 * 24 * time.Hour // ~3 years
	}
	if c.Interval <= 0 {
		c.Interval = 1 * time.Hour
	}
}

// GC prunes old audit rows and writes a checkpoint before each prune.
type GC struct {
	store Store
	cfg   GCConfig
	log   *slog.Logger

	mu sync.Mutex // guards cfg.Retention against concurrent UpdateRetention/Tick
}

// NewGC creates a GC with the given store, config, and logger.
// Zero-value config fields are filled with defaults.
func NewGC(store Store, cfg GCConfig, log *slog.Logger) *GC {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	return &GC{store: store, cfg: cfg, log: log}
}

// UpdateRetention atomically swaps the retention window. Safe to call
// concurrently with Tick; the next Tick observes the new value. Called
// from the config hot-reload callback (spec §8 lists retention as
// hot-reloadable). A non-positive duration is ignored (defensive).
func (g *GC) UpdateRetention(d time.Duration) {
	if d <= 0 {
		return
	}
	g.mu.Lock()
	g.cfg.Retention = d
	g.mu.Unlock()
}

// Retention returns the currently effective retention window. Useful for
// tests and observability.
func (g *GC) Retention() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cfg.Retention
}

// Run launches a background ticker that calls Tick on each interval.
// Returns when ctx is cancelled.
func (g *GC) Run(ctx context.Context) {
	ticker := time.NewTicker(g.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := g.Tick(ctx); err != nil {
				g.log.Error("audit gc: tick failed", "err", err)
			}
		}
	}
}

// Tick performs one GC pass. Returns (deleted, err).
//
// Algorithm per spec section 5.5/5.7, executed as a SINGLE transaction:
//  1. cutoff := now - retention
//  2. Begin a write Tx (holds writeMu on SQLite / pg_advisory_xact_lock on PG
//     until Commit — same serialization as the append path)
//  3. Find the last row to be pruned (highest id with ts < cutoff)
//  4. If none: rollback, return 0
//  5. Delete every row with id <= that row's id (keyed off id, the true
//     monotonic order, so equal-ms timestamps can't cause off-by-one)
//  6. If the table is now empty, set LastSelfHash="" so the next append is genesis
//  7. Write the checkpoint inside the same Tx, then Commit
//
// Running the whole prune under one Tx closes the C1/C2 race: previously the
// find → checkpoint → delete → checkpoint ran as separate store calls, each
// acquiring and releasing the writer lock independently. A concurrent
// flushBatch could insert a row between the checkpoint and the delete whose
// self_hash became part of the chain but whose row was then pruned, breaking
// the chain and producing a false-positive tamper alert. Now no other writer
// can interleave between the anchor read and the prune commit.
func (g *GC) Tick(ctx context.Context) (int64, error) {
	g.mu.Lock()
	retention := g.cfg.Retention
	g.mu.Unlock()
	cutoff := time.Now().Add(-retention)

	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("audit gc: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 1. Find the highest-id row with ts < cutoff. Returns (0,"",nil) if none.
	lastID, lastHash, err := tx.LastRowBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("audit gc: last row before: %w", err)
	}
	if lastID == 0 {
		return 0, nil // nothing to prune
	}

	// 2. Delete every row up to and including lastID. The id boundary is the
	// exact set the checkpoint will anchor — no ts-based off-by-one.
	deleted, err := tx.DeleteByIDLEQ(ctx, lastID)
	if err != nil {
		return 0, fmt.Errorf("audit gc: delete: %w", err)
	}

	// 3. Build the checkpoint. If the table is now empty, clear LastSelfHash
	// so the verifier treats the next appended row as genesis (prev_hash="").
	cp := Checkpoint{
		PrunedAt:     time.Now(),
		LastSelfHash: lastHash,
		NextID:       lastID + 1, // first surviving row's id
	}
	remaining, err := tx.RowCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("audit gc: row count: %w", err)
	}
	if remaining == 0 {
		cp.LastSelfHash = ""
	}

	if err := tx.SaveCheckpoint(ctx, cp); err != nil {
		return 0, fmt.Errorf("audit gc: save checkpoint: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("audit gc: commit: %w", err)
	}
	committed = true

	g.log.Info("audit gc: pruned",
		"deleted", deleted,
		"checkpoint_next_id", cp.NextID,
		"last_self_hash", cp.LastSelfHash,
	)
	return deleted, nil
}
