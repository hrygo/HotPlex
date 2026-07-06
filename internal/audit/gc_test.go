package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeChain writes N chained audit rows with proper hash linking.
// Each row gets a timestamp 1ms apart starting from now.
func writeChain(t *testing.T, store Store, n int) {
	t.Helper()
	base := time.Now().UnixMilli()
	tss := make([]int64, n)
	for i := 0; i < n; i++ {
		tss[i] = base + int64(i)
	}
	writeChainWithTimestamps(t, store, tss)
}

// writeChainWithTimestamps writes N chained audit rows with explicit timestamps.
func writeChainWithTimestamps(t *testing.T, store Store, timestamps []int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	tail, err := tx.TailHash(ctx)
	require.NoError(t, err)
	for i, ts := range timestamps {
		ua := &UserActivity{
			Ts:         ts,
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
		_ = i
	}
	require.NoError(t, tx.Commit())
}

func TestGC_PruneOldRows_WritesCheckpoint(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Use explicit timestamps well in the past (1 hour ago) to ensure they're pruned.
	baseTs := time.Now().Add(-1 * time.Hour).UnixMilli()
	tss := make([]int64, 5)
	for i := 0; i < 5; i++ {
		tss[i] = baseTs + int64(i)
	}
	writeChainWithTimestamps(t, store, tss)

	// Retention 1ms: all rows are old (created 1 hour ago).
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond, Interval: time.Hour}, nil)
	deleted, err := gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), deleted)

	// Checkpoint was written.
	cp, err := store.LatestCheckpoint(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, int64(6), cp.NextID) // first surviving row id would be 6
	// After full prune, LastSelfHash is "" (genesis marker for next row).
	require.Empty(t, cp.LastSelfHash)
}

func TestGC_VerifyAfterFullPrune(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Use explicit timestamps well in the past.
	baseTs := time.Now().Add(-1 * time.Hour).UnixMilli()
	tss := make([]int64, 5)
	for i := 0; i < 5; i++ {
		tss[i] = baseTs + int64(i)
	}
	writeChainWithTimestamps(t, store, tss)

	// Prune all rows.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond}, nil)
	_, err := gc.Tick(context.Background())
	require.NoError(t, err)

	// Verify chain from latest checkpoint — no surviving rows, so 0 checked.
	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "chain should verify after full prune")
	require.Empty(t, result.Reason)
	require.Equal(t, 0, result.RowsChecked)
}

func TestGC_EmptyPrune(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)

	// No rows to prune.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond}, nil)
	deleted, err := gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)

	cp, err := store.LatestCheckpoint(context.Background())
	require.NoError(t, err)
	require.Nil(t, cp)
}

func TestGC_PartialPrune_VerifySurvivors(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)

	// Write 5 rows: first 3 are 2 hours old, last 2 are 1 minute old.
	now := time.Now()
	oldTs := now.Add(-2 * time.Hour).UnixMilli()
	newTs := now.Add(-1 * time.Minute).UnixMilli()
	tss := []int64{oldTs, oldTs + 1, oldTs + 2, newTs, newTs + 1}
	writeChainWithTimestamps(t, store, tss)

	// Retention 1 hour: cutoff = now - 1 hour.
	// Rows 1-3 (2 hours old) are pruned, rows 4-5 (1 minute old) survive.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Hour}, nil)
	deleted, err := gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)

	// Verify surviving chain.
	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "surviving chain should verify")
	require.Empty(t, result.Reason)
	require.Equal(t, 2, result.RowsChecked)
}

func TestGC_FullPruneThenNewRow_Verify(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Use explicit timestamps well in the past.
	baseTs := time.Now().Add(-1 * time.Hour).UnixMilli()
	tss := make([]int64, 3)
	for i := 0; i < 3; i++ {
		tss[i] = baseTs + int64(i)
	}
	writeChainWithTimestamps(t, store, tss)

	// Prune all.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond}, nil)
	_, err := gc.Tick(context.Background())
	require.NoError(t, err)

	// Append a new row — should be genesis (prev_hash = "").
	writeChain(t, store, 1)

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "first row after full prune should be genesis")
	require.Empty(t, result.Reason)
	require.Equal(t, 1, result.RowsChecked)
}

func TestGC_Defaults(t *testing.T) {
	t.Parallel()
	cfg := GCConfig{}
	cfg.defaults()
	require.Equal(t, 3*365*24*time.Hour, cfg.Retention)
	require.Equal(t, 1*time.Hour, cfg.Interval)
}
