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

// VerifyOnce runs a single chain verification pass from the latest checkpoint.
// Returns VerifyResult with chain status.
func (v *Verifier) VerifyOnce(ctx context.Context) (VerifyResult, error) {
	cp, err := v.store.LatestCheckpoint(ctx)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("audit verify: latest checkpoint: %w", err)
	}

	// Collect all rows via pagination (Query returns DESC, we need ASC).
	allRows, err := v.collectAllRows(ctx)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("audit verify: collect rows: %w", err)
	}

	// If we have a checkpoint, filter to rows with id >= cp.NextID.
	if cp != nil {
		filtered := make([]UserActivity, 0, len(allRows))
		for i := range allRows {
			if allRows[i].ID >= cp.NextID {
				filtered = append(filtered, allRows[i])
			}
		}
		allRows = filtered
	}

	if len(allRows) == 0 {
		return VerifyResult{Checkpoint: cp, RowsChecked: 0}, nil
	}

	// Determine the checkpoint hash for VerifyChain.
	checkpointHash := ""
	if cp != nil {
		checkpointHash = cp.LastSelfHash
	}

	// Walk the chain using the existing VerifyChain function.
	brokenID, reason := VerifyChain(allRows, checkpointHash)
	if brokenID != 0 {
		observability.AuditChainBreaks().Add(ctx, 1,
			metric.WithAttributes(attribute.String("reason", reason)))
	}

	return VerifyResult{
		Checkpoint:  cp,
		RowsChecked: len(allRows),
		BrokenID:    brokenID,
		Reason:      reason,
	}, nil
}

// collectAllRows paginates through all rows in the store and returns them
// in ascending ID order. Query returns DESC, so we collect all batches
// and then reverse the entire result.
func (v *Verifier) collectAllRows(ctx context.Context) ([]UserActivity, error) {
	const batchSize = 1000 // max allowed by Query
	var allRows []UserActivity
	offset := 0
	for {
		batch, err := v.store.Query(ctx, Query{Limit: batchSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		allRows = append(allRows, batch...)
		if len(batch) < batchSize {
			break
		}
		offset += batchSize
	}
	// Reverse to get ASC order (Query returns DESC)
	for i, j := 0, len(allRows)-1; i < j; i, j = i+1, j-1 {
		allRows[i], allRows[j] = allRows[j], allRows[i]
	}
	return allRows, nil
}
