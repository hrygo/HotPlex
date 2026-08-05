// Package audit — rebase.go implements the operator-initiated chain
// re-anchor used to repair a broken hash chain. See audit.md §5.4.
package audit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrRebaseRowNotFound is returned when the rebase target id has no surviving
// row: the row was deleted, or the id is past the table tail. Rebase must
// never silently anchor at a different row.
var ErrRebaseRowNotFound = errors.New("audit: rebase target row not found")

// Rebase re-anchors the hash chain at the row with the given id, returning
// the written checkpoint. The target row's stored prev_hash becomes the new
// checkpoint anchor and VerifyOnce continues from this row: rows before the
// anchor are retained but no longer chain-verified.
//
// This is the only legitimate repair for a chain broken by a historical
// manual DELETE (migrations 023/030 reject row UPDATE and un-anchored DELETE,
// so a broken row cannot be edited or removed in place). The written
// checkpoint carries the same semantics as a GC checkpoint-anchored prune:
// the chain is declared valid from the anchor onward.
func Rebase(ctx context.Context, store Store, nextID int64) (Checkpoint, error) {
	rows, err := store.QueryAsc(ctx, nextID, 1)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("audit rebase: query target row: %w", err)
	}
	if len(rows) == 0 || rows[0].ID != nextID {
		return Checkpoint{}, fmt.Errorf("%w: id=%d", ErrRebaseRowNotFound, nextID)
	}
	cp := Checkpoint{
		PrunedAt:     time.Now(),
		LastSelfHash: rows[0].PrevHash,
		NextID:       nextID,
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		return Checkpoint{}, fmt.Errorf("audit rebase: save checkpoint: %w", err)
	}
	return cp, nil
}
