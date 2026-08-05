package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRebase_RepairsBrokenChain covers the operator re-anchor contract: a
// chain broken by a historical manual DELETE (the broken_id=1253/1269 era)
// cannot be repaired by editing rows (triggers reject UPDATE/DELETE), so the
// operator rebases the chain at the first surviving row — its stored
// prev_hash becomes the new checkpoint anchor and verify continues from
// there. Rows before the anchor are retained but no longer chain-verified.
func TestRebase_RepairsBrokenChain(t *testing.T) {
	t.Parallel()
	store, db := newTestStoreAndDB(t)
	writeChain(t, store, 6)

	ctx := context.Background()
	// Interior delete mirrors the historical manual-DELETE breakage: row 3
	// vanishes, orphaning row 4 (its prev_hash references the deleted row).
	_, err := db.ExecContext(ctx, `DELETE FROM user_activity WHERE id = 3`)
	require.NoError(t, err)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.BrokenID, "precondition: chain must be broken at row 4")

	// Rebase at row 4: row 4's own prev_hash becomes the anchor.
	cp, err := Rebase(ctx, store, 4)
	require.NoError(t, err)
	require.Equal(t, int64(4), cp.NextID, "checkpoint must start at the anchor row")
	require.NotEmpty(t, cp.LastSelfHash, "anchor hash must be the row's stored prev_hash")

	result, err = v.VerifyOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "rebase must restore integrity from the anchor row")
	require.Equal(t, 3, result.RowsChecked, "rows 4..6 are verified from the new anchor")
}

// TestRebase_KeepsAppendChainWorking ensures the append path (which links to
// the table tail, not the checkpoint) keeps producing a healthy chain after
// a rebase — new rows chain onto the last surviving row regardless of the
// re-anchor.
func TestRebase_KeepsAppendChainWorking(t *testing.T) {
	t.Parallel()
	store, db := newTestStoreAndDB(t)
	writeChain(t, store, 4)

	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DELETE FROM user_activity WHERE id = 2`)
	require.NoError(t, err)

	_, err = Rebase(ctx, store, 3)
	require.NoError(t, err)

	// Append two more linked rows via the normal tx.Append path.
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	tail, err := tx.TailHash(ctx)
	require.NoError(t, err)
	for i := range 2 {
		ua := &UserActivity{
			Ts:         2000000000000 + int64(i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: "{}",
			PrevHash:   tail,
		}
		h, err := ComputeSelfHash(tail, ua)
		require.NoError(t, err)
		ua.SelfHash = h
		require.NoError(t, tx.Append(ctx, ua))
		tail = h
	}
	require.NoError(t, tx.Commit())

	result, err := NewVerifier(store, VerifierConfig{}, nil).VerifyOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "append after rebase must stay chained")
}

// TestRebase_GenesisAnchor covers rebasing at the very first row: its empty
// prev_hash makes the checkpoint anchor empty, so verify treats the anchor
// row as the new genesis.
func TestRebase_GenesisAnchor(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	writeChain(t, store, 3)

	ctx := context.Background()
	cp, err := Rebase(ctx, store, 1)
	require.NoError(t, err)
	require.Empty(t, cp.LastSelfHash, "genesis row has an empty prev_hash")
	require.Equal(t, int64(1), cp.NextID)

	result, err := NewVerifier(store, VerifierConfig{}, nil).VerifyOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID)
}

// TestRebase_TargetRowMissing pins the failure contract: rebasing to an id
// with no surviving row (the row was deleted, or the table is empty) must
// fail with ErrRebaseRowNotFound instead of silently anchoring elsewhere.
func TestRebase_TargetRowMissing(t *testing.T) {
	t.Parallel()
	store, db := newTestStoreAndDB(t)
	writeChain(t, store, 3)

	ctx := context.Background()
	// Row 2 deleted AND target 2 beyond the tail: QueryAsc(2) would return
	// row 3, which is NOT the requested anchor — must be rejected.
	_, err := db.ExecContext(ctx, `DELETE FROM user_activity WHERE id = 2`)
	require.NoError(t, err)
	_, err = Rebase(ctx, store, 2)
	require.ErrorIs(t, err, ErrRebaseRowNotFound)

	// Past the tail entirely.
	_, err = Rebase(ctx, store, 99)
	require.ErrorIs(t, err, ErrRebaseRowNotFound)
}

// TestRebase_EmptyTable pins the empty-table failure contract.
func TestRebase_EmptyTable(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)

	_, err := Rebase(context.Background(), store, 1)
	require.ErrorIs(t, err, ErrRebaseRowNotFound)
}
