// Package audit — verify.go implements chain verification with checkpoint rebase.
// See spec §10 (chain verification).
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/observability"
)

// VerifierConfig holds verifier tunables.
type VerifierConfig struct {
	Interval time.Duration // default 1h
}

func (c *VerifierConfig) defaults() {
	if c.Interval <= 0 {
		c.Interval = 1 * time.Hour
	}
}

// VerifyResult is the outcome of a single verify pass.
type VerifyResult struct {
	Checkpoint  *Checkpoint // the anchor used (nil if no checkpoint)
	RowsChecked int
	BrokenID    int64  // 0 if intact
	Reason      string // "" if intact
}

// Verifier periodically re-verifies the chain from the latest checkpoint.
type Verifier struct {
	store Store
	cfg   VerifierConfig
	log   *slog.Logger
}

// NewVerifier creates a Verifier with the given store, config, and logger.
// Zero-value config fields are filled with defaults.
func NewVerifier(store Store, cfg VerifierConfig, log *slog.Logger) *Verifier {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	return &Verifier{store: store, cfg: cfg, log: log}
}

// Run launches a background ticker that calls VerifyOnce on each interval.
func (v *Verifier) Run(ctx context.Context) {
	ticker := time.NewTicker(v.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := v.VerifyOnce(ctx)
			if err != nil {
				v.log.Error("audit verify: pass failed", "err", err)
				continue
			}
			if result.BrokenID != 0 {
				v.log.Warn("audit verify: chain break detected",
					"broken_id", result.BrokenID, "reason", result.Reason)
			}
		}
	}
}

// VerifyOnce runs a single streaming chain verification pass from the
// latest checkpoint. It pages through the store in ascending id order
// holding only one batch in memory at a time, so memory use is
// O(batchSize) regardless of table size (prevents OOM at 3-year retention
// scale). Spec §5.5 chain verification.
//
// Verification walks the hash chain forward: starting from the
// checkpoint's LastSelfHash (or "" if no checkpoint), each row's
// prev_hash must equal the running cursor and its self_hash must equal
// ComputeSelfHash(cursor, row). The first violation short-circuits.
func (v *Verifier) VerifyOnce(ctx context.Context) (VerifyResult, error) {
	cp, err := v.store.LatestCheckpoint(ctx)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("audit verify: latest checkpoint: %w", err)
	}

	const batchSize = 1000

	// Anchor: the hash the first surviving row's prev_hash must equal.
	cursor := ""
	fromID := int64(1) // ids start at 1
	if cp != nil {
		cursor = cp.LastSelfHash
		fromID = cp.NextID
	}

	var rowsChecked int
	for {
		batch, err := v.store.QueryAsc(ctx, fromID, batchSize)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("audit verify: query_asc: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		// Verify this batch against the running cursor. Each row links to
		// the previous one; the batch starts from the cursor carried over
		// from the prior batch (or the checkpoint).
		brokenID, reason := VerifyChain(batch, cursor)
		if brokenID != 0 {
			observability.AuditChainBreaks().Add(ctx, 1,
				metric.WithAttributes(attribute.String("reason", reason)))
			return VerifyResult{
				Checkpoint:  cp,
				RowsChecked: rowsChecked,
				BrokenID:    brokenID,
				Reason:      reason,
			}, nil
		}
		rowsChecked += len(batch)
		// Advance cursor and fromID to just past this batch.
		cursor = batch[len(batch)-1].SelfHash
		fromID = batch[len(batch)-1].ID + 1
		if len(batch) < batchSize {
			break
		}
	}

	return VerifyResult{
		Checkpoint:  cp,
		RowsChecked: rowsChecked,
		BrokenID:    0,
		Reason:      "",
	}, nil
}
