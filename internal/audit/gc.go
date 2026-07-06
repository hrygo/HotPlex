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
// Algorithm per spec section 5.5/5.7:
//  1. cutoff := now - retention
//  2. Find the last row to be pruned (highest id with ts < cutoff)
//  3. If found: write a checkpoint with its self_hash + next_id
//  4. DELETE rows where ts < cutoff (using last pruned row's ts + 1ms as boundary)
//  5. First-prune edge case: if 0 rows survive, next row's prev_hash should be "" (genesis)
func (g *GC) Tick(ctx context.Context) (int64, error) {
	g.mu.Lock()
	retention := g.cfg.Retention
	g.mu.Unlock()
	cutoff := time.Now().Add(-retention)

	// 1. Find the last row to be pruned (highest id with ts <= cutoff).
	// Query returns rows in DESC order, so Limit=1 gives us the highest-id row.
	rows, err := g.store.Query(ctx, Query{
		To:    cutoff,
		Limit: 1,
	})
	if err != nil {
		return 0, fmt.Errorf("audit gc: query last-to-prune: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil // nothing to prune
	}

	lastPruned := rows[0] // DESC order: first row is the highest-id match

	// 2. Write checkpoint BEFORE deleting (so verifier can always rebase).
	cp := Checkpoint{
		PrunedAt:     time.Now(),
		LastSelfHash: lastPruned.SelfHash,
		NextID:       lastPruned.ID + 1, // first surviving row's id
	}
	if err := g.store.SaveCheckpoint(ctx, cp); err != nil {
		return 0, fmt.Errorf("audit gc: save checkpoint: %w", err)
	}

	// 3. Delete old rows. Use lastPruned.Ts + 1ms as the delete boundary
	// to ensure exactly the same set of rows is deleted as was used for
	// the checkpoint (avoids off-by-one between Query's <= and DeleteBefore's <).
	deleteBoundary := time.UnixMilli(lastPruned.Ts + 1)
	deleted, err := g.store.DeleteBefore(ctx, deleteBoundary)
	if err != nil {
		return 0, fmt.Errorf("audit gc: delete: %w", err)
	}

	// 4. If all rows were pruned (DB is now empty), update the checkpoint
	// to set LastSelfHash="" so the next row is treated as genesis.
	remaining, err := g.store.Query(ctx, Query{Limit: 1})
	if err != nil {
		return 0, fmt.Errorf("audit gc: query remaining: %w", err)
	}
	if len(remaining) == 0 {
		// DB is empty — update checkpoint to indicate genesis.
		cp.LastSelfHash = ""
		if err := g.store.SaveCheckpoint(ctx, cp); err != nil {
			return 0, fmt.Errorf("audit gc: update checkpoint for genesis: %w", err)
		}
	}

	g.log.Info("audit gc: pruned",
		"deleted", deleted,
		"checkpoint_next_id", cp.NextID,
		"last_self_hash", cp.LastSelfHash,
	)
	return deleted, nil
}
