package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerify_ValidChain(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	writeChain(t, store, 10)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID)
	require.Empty(t, result.Reason)
	require.Equal(t, 10, result.RowsChecked)
}

func TestVerify_TamperedRowDetected(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	writeChain(t, store, 5)

	// Insert a row with bad hashes directly via the store (bypassing hash computation).
	// The store does not validate hashes on insert.
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	badRow := &UserActivity{
		Ts:         9999999999999,
		UserID:     "u1",
		UserIDType: UserIDTypePlatform,
		Platform:   PlatformTest,
		Action:     ActionAuthLogin,
		Outcome:    OutcomeSuccess,
		DetailJSON: "{}",
		PrevHash:   "wrong-prev-hash",
		SelfHash:   "wrong-self-hash",
	}
	require.NoError(t, tx.Append(ctx, badRow))
	require.NoError(t, tx.Commit())

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, int64(0), result.BrokenID, "should detect broken row")
	require.NotEmpty(t, result.Reason)
}

func TestVerify_AfterCheckpoint(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	writeChain(t, store, 5)

	// Save a checkpoint with a fake LastSelfHash that doesn't match any row.
	// NextID=4 means verifier will check rows with id >= 4.
	// Row 4's prev_hash is the actual row 3's self_hash, not "fake-hash",
	// so the verifier should detect a mismatch.
	err := store.SaveCheckpoint(context.Background(), Checkpoint{
		PrunedAt:     testNow(),
		LastSelfHash: "fake-hash",
		NextID:       4,
	})
	require.NoError(t, err)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, int64(0), result.BrokenID, "should detect break due to checkpoint mismatch")
	require.Equal(t, "prev_hash_mismatch", result.Reason)
}

func TestVerify_EmptyDB(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.RowsChecked)
	require.Equal(t, int64(0), result.BrokenID)
	require.Empty(t, result.Reason)
}

func TestVerify_FirstPrune(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Use explicit timestamps well in the past.
	baseTs := time.Now().Add(-1 * time.Hour).UnixMilli()
	tss := make([]int64, 3)
	for i := 0; i < 3; i++ {
		tss[i] = baseTs + int64(i)
	}
	writeChainWithTimestamps(t, store, tss)

	// Prune all rows.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond}, nil)
	_, err := gc.Tick(context.Background())
	require.NoError(t, err)

	// Append a new row — should be genesis (prev_hash = "").
	writeChain(t, store, 1)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "first row after full prune should verify")
	require.Empty(t, result.Reason)
	require.Equal(t, 1, result.RowsChecked)
}

func TestVerify_Defaults(t *testing.T) {
	t.Parallel()
	cfg := VerifierConfig{}
	cfg.defaults()
	require.Equal(t, time.Hour, cfg.Interval)
}

// TestVerify_LargeTablePagesWithoutLoadingAll verifies the streaming
// verifier (review issue #5) correctly verifies a chain larger than one
// batch size without loading all rows into memory at once. We write 2.5x
// the batch size (2500 rows vs 1000 batch) and confirm verification
// passes and counts every row.
func TestVerify_LargeTablePagesWithoutLoadingAll(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	const n = 2500 // 2.5x the verifier's internal batchSize of 1000
	writeChain(t, store, n)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "chain should verify intact")
	require.Empty(t, result.Reason)
	require.Equal(t, n, result.RowsChecked, "streaming verifier must count all rows across batches")
}

// TestVerify_LargeTable_TamperedRowDetectedMidChain verifies the streaming
// verifier still detects tampering when the broken row is in a later batch
// (i.e. the inter-batch prev_hash carry-forward works correctly).
func TestVerify_LargeTable_TamperedRowDetectedMidChain(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Write 1500 good rows (more than one batch), then a bad row.
	writeChain(t, store, 1500)

	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	badRow := &UserActivity{
		Ts:         9999999999999,
		UserID:     "attacker",
		UserIDType: UserIDTypePlatform,
		Platform:   PlatformTest,
		Action:     ActionAuthLogin,
		Outcome:    OutcomeSuccess,
		DetailJSON: "{}",
		PrevHash:   "wrong-prev", // doesn't link to the previous row's self_hash
		SelfHash:   "wrong-self",
	}
	require.NoError(t, tx.Append(ctx, badRow))
	require.NoError(t, tx.Commit())

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(ctx)
	require.NoError(t, err)
	require.NotEqual(t, int64(0), result.BrokenID, "should detect tampered row in second batch")
	require.Equal(t, "prev_hash_mismatch", result.Reason)
	// The bad row lives past the first batch boundary (id 1501 > batch 1000),
	// so the verifier must have crossed at least one batch boundary before
	// hitting it. RowsChecked reflects rows verified before the break, which
	// includes the entire first batch (1000 good rows).
	require.GreaterOrEqual(t, result.RowsChecked, 1000, "verifier must page past the first batch")
	// The tampered row is the 1501st (1500 good + 1 bad).
	require.Equal(t, int64(1501), result.BrokenID, "broken row id must be the tampered row")
}

// testNow returns the current time (helper for readability).
func testNow() time.Time {
	return time.Now()
}
