// Package audit — verify.go implements chain verification with checkpoint rebase.
// See spec §10 (chain verification).
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	BrokenID    int64  // first break's row id; 0 if intact
	Reason      string // first break's reason; "" if intact
	// Broken carries non-PII diagnostics about the FIRST broken row (nil when
	// intact). PII fields (UserID/SessionID/IP/UserAgent/DetailJSON) are
	// deliberately excluded so the diagnostic log stays safe to emit.
	Broken *BrokenRowInfo
	// BrokenRows lists EVERY break found in one pass (not just the first),
	// so operators see the full extent of a chain break. The historical
	// short-circuit hid later gaps — e.g. the id=1269 break was masked by
	// the first break at 1253 until the earlier one was fixed.
	BrokenRows []BrokenRowInfo
}

// BrokenRowInfo is the non-PII diagnostic snapshot of the broken row,
// attached to VerifyResult so the first WARN can pinpoint where the chain
// broke without leaking user data.
type BrokenRowInfo struct {
	ID           int64  // broken row id
	Ts           int64  // row timestamp, Unix ms
	Platform     string // webchat/feishu/slack/admin/api/cron
	Action       string // auth.login, session.create, …
	Outcome      string // success/failure/denied
	ResourceType string // optional resource category
	// Reason is the violation kind (prev_hash_mismatch, self_hash_mismatch,
	// compute_error:…).
	Reason string
	// ExpectedPrevHash is the hash the row's prev_hash should equal (the
	// chain cursor carried from the previous row or checkpoint anchor).
	// Populated for prev_hash_mismatch breaks only.
	ExpectedPrevHash string
	// ActualPrevHash is the prev_hash stored in the row. When the two
	// differ the gap lies before this row: the previous row was deleted,
	// modified, or never linked correctly.
	ActualPrevHash string
	// ExpectedSelfHash is the self_hash the row SHOULD carry, recomputed
	// from the row's own stored prev_hash. Populated for self_hash_mismatch
	// breaks only (tampering) — it is a self hash, not a prev hash, so it
	// must not be conflated with the ExpectedPrevHash fields above.
	ExpectedSelfHash string
	// ActualSelfHash is the self_hash stored in the row. When it differs
	// from ExpectedSelfHash the row content changed after insert.
	ActualSelfHash string
}

// Verifier periodically re-verifies the chain from the latest checkpoint.
type Verifier struct {
	store Store
	cfg   VerifierConfig
	log   *slog.Logger

	// mu guards the recurring-break dedup state below. Only Run writes it,
	// but VerifyOnce is exported, so keep the access explicit.
	mu          sync.Mutex
	lastBroken  int64
	firstSeen   time.Time
	occurrences int
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
			v.recordResult(result)
		}
	}
}

// recordResult logs the outcome of a verify pass. A new chain break is
// logged at WARN with the row context; a break that persists across passes
// is downgraded to DEBUG with first_seen/occurrences so a recurring
// condition does not turn into an hourly alert storm; a break that clears
// logs an INFO resolution.
func (v *Verifier) recordResult(result VerifyResult) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if result.BrokenID == 0 {
		if v.lastBroken != 0 {
			v.log.Info("audit verify: chain break resolved",
				"broken_id", v.lastBroken, "occurrences", v.occurrences)
			v.lastBroken = 0
			v.firstSeen = time.Time{}
			v.occurrences = 0
		}
		return
	}

	if result.BrokenID == v.lastBroken {
		v.occurrences++
		v.log.Debug("audit verify: chain break persists",
			"broken_id", result.BrokenID, "reason", result.Reason,
			"first_seen", v.firstSeen.Format(time.RFC3339), "occurrences", v.occurrences)
		return
	}

	v.lastBroken = result.BrokenID
	v.firstSeen = time.Now()
	v.occurrences = 1
	attrs := []any{
		"broken_id", result.BrokenID,
		"reason", result.Reason,
		"rows_checked", result.RowsChecked,
		"advice", BreakAdvice(result.Reason),
	}
	if len(result.BrokenRows) > 1 {
		ids := make([]int64, 0, len(result.BrokenRows))
		for _, b := range result.BrokenRows {
			ids = append(ids, b.ID)
		}
		attrs = append(attrs,
			"broken_count", len(result.BrokenRows),
			"broken_ids", ids,
		)
	}
	if b := result.Broken; b != nil {
		attrs = append(attrs,
			"broken_at", time.UnixMilli(b.Ts).Format(time.RFC3339),
			"platform", b.Platform,
			"action", b.Action,
			"outcome", b.Outcome,
			"resource_type", b.ResourceType,
		)
		// Per-reason hash diagnostics: prev-hash linkage for gap breaks,
		// self hashes for tamper breaks (never conflated).
		if strings.HasPrefix(result.Reason, "prev_hash_mismatch") {
			attrs = append(attrs,
				"expected_prev_hash", b.ExpectedPrevHash,
				"actual_prev_hash", b.ActualPrevHash,
			)
		} else {
			attrs = append(attrs,
				"expected_self_hash", b.ExpectedSelfHash,
				"actual_self_hash", b.ActualSelfHash,
			)
		}
	}
	v.log.Warn("audit verify: chain break detected", attrs...)
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
// ComputeSelfHash(cursor, row). Every violation is collected (up to
// MaxBrokenRows) — the cursor keeps advancing past a break so later
// breaks are not masked by earlier ones.
func (v *Verifier) VerifyOnce(ctx context.Context) (VerifyResult, error) {
	cp, err := v.store.LatestCheckpoint(ctx)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("audit verify: latest checkpoint: %w", err)
	}

	const batchSize = 1000
	const maxBroken = 50

	// Anchor: the hash the first surviving row's prev_hash must equal.
	cursor := ""
	fromID := int64(1) // ids start at 1
	if cp != nil {
		cursor = cp.LastSelfHash
		fromID = cp.NextID
	}

	var rowsChecked int
	var brokenRows []BrokenRowInfo
	for {
		batch, err := v.store.QueryAsc(ctx, fromID, batchSize)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("audit verify: query_asc: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		breaks := VerifyChain(batch, cursor)
		for _, b := range breaks {
			if len(brokenRows) >= maxBroken {
				break
			}
			info := BrokenRowInfo{
				ID:     b.ID,
				Reason: b.Reason,
			}
			if strings.HasPrefix(b.Reason, "prev_hash_mismatch") {
				info.ExpectedPrevHash = b.Expected
				info.ActualPrevHash = b.Actual
			} else {
				info.ExpectedSelfHash = b.Expected
				info.ActualSelfHash = b.Actual
			}
			for i := range batch {
				if batch[i].ID != b.ID {
					continue
				}
				info.Ts = batch[i].Ts
				info.Platform = batch[i].Platform
				info.Action = batch[i].Action
				info.Outcome = batch[i].Outcome
				info.ResourceType = batch[i].ResourceType
				break
			}
			brokenRows = append(brokenRows, info)
		}
		rowsChecked += len(batch)
		// Advance cursor and fromID to just past this batch regardless of
		// breaks, so later breaks in subsequent batches are still found.
		cursor = batch[len(batch)-1].SelfHash
		fromID = batch[len(batch)-1].ID + 1
		if len(batch) < batchSize {
			break
		}
	}

	if len(brokenRows) > 0 {
		first := brokenRows[0]
		observability.AuditChainBreaks().Add(ctx, 1,
			metric.WithAttributes(attribute.String("reason", first.Reason)))
		return VerifyResult{
			Checkpoint:  cp,
			RowsChecked: rowsChecked,
			BrokenID:    first.ID,
			Reason:      first.Reason,
			Broken:      &first,
			BrokenRows:  brokenRows,
		}, nil
	}

	return VerifyResult{
		Checkpoint:  cp,
		RowsChecked: rowsChecked,
		BrokenID:    0,
		Reason:      "",
	}, nil
}

// BreakAdvice maps a verify failure reason to actionable remediation
// guidance, so the first WARN tells the operator what the break means and
// what to do instead of only exposing a raw hash mismatch.
func BreakAdvice(reason string) string {
	switch {
	case strings.HasPrefix(reason, "prev_hash_mismatch"):
		return "chain gap before this row: previous row missing/modified, or legacy GC-race false positive; false positives auto-heal after retention GC rebase, real tamper needs user_activity restore from backup"
	case strings.HasPrefix(reason, "self_hash_mismatch"):
		return "row content changed after insert (tamper): restore user_activity from backup"
	default:
		return "hash recomputation failed: verify canonical serialization compatibility with the version that wrote the row"
	}
}
