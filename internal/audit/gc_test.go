package audit

import (
	"context"
	"sync"
	"sync/atomic"
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

// TestGC_UpdateRetention_HotReload verifies that UpdateRetention atomically
// swaps the effective retention and the next Tick observes it. This is the
// regression for review issue #4 (config watcher accepted audit.retention
// but never propagated the value to the GC instance).
func TestGC_UpdateRetention_HotReload(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	// Write a row that is 10 minutes old — old enough for any sub-10m retention.
	oldTs := time.Now().Add(-10 * time.Minute).UnixMilli()
	writeChainWithTimestamps(t, store, []int64{oldTs})

	// Start GC at 3-year retention; the 10-minute-old row survives.
	gc := NewGC(store, GCConfig{Retention: 3 * 365 * 24 * time.Hour}, nil)
	require.Equal(t, 3*365*24*time.Hour, gc.Retention())

	deleted, err := gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted, "3-year retention should keep the 10m-old row")

	// Hot-reload to 1-minute retention; now the row should be pruned.
	gc.UpdateRetention(1 * time.Minute)
	require.Equal(t, 1*time.Minute, gc.Retention())

	deleted, err = gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "1-minute retention should prune the 10m-old row")
}

// TestGC_UpdateRetention_IgnoresNonPositive verifies defensive clamping —
// a zero or negative duration must not wipe the entire table.
func TestGC_UpdateRetention_IgnoresNonPositive(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	writeChain(t, store, 3)

	gc := NewGC(store, GCConfig{Retention: 1 * time.Hour}, nil)
	gc.UpdateRetention(0)
	gc.UpdateRetention(-5 * time.Second)
	require.Equal(t, 1*time.Hour, gc.Retention(), "non-positive update must be ignored")

	deleted, err := gc.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)
}

// TestGC_ConcurrentWritesPreserveChain is the regression test for review
// findings C1/C2. Before the fix, GC.Tick ran find→checkpoint→delete→checkpoint
// as separate store calls, each acquiring and releasing the writer lock
// independently. A concurrent flushBatch could insert a row between the
// checkpoint anchor read and the delete whose self_hash became part of the
// chain but whose row was then pruned — silently breaking the chain and
// producing a false-positive tamper alert.
//
// This test drives many concurrent appends (each its own BeginTx→Append→Commit,
// mirroring the collector's flush path) while a GC Tick prunes old rows in the
// middle, then asserts the verifier still sees an intact chain (BrokenID==0).
// Uses a connection pool > 1 so concurrent transactions genuinely contend for
// SQLite's single writer slot instead of being serialized by the pool.
func TestGC_ConcurrentWritesPreserveChain(t *testing.T) {
	t.Parallel()

	// Pool 3 mirrors production (MaxOpenConns=3); without the per-Tx writeMu
	// two concurrent write txns on different connections surface SQLITE_BUSY.
	store := newTestSQLiteStoreWithPool(t, 3)

	// Seed with a chain of old rows (1 hour ago) so GC has something to prune.
	oldBase := time.Now().Add(-1 * time.Hour).UnixMilli()
	oldTss := make([]int64, 5)
	for i := range oldTss {
		oldTss[i] = oldBase + int64(i)
	}
	writeChainWithTimestamps(t, store, oldTss)

	// GC prunes everything older than 1ms — the seeded old rows qualify,
	// the concurrently-written "now" rows do not.
	gc := NewGC(store, GCConfig{Retention: 1 * time.Millisecond}, nil)

	// appendOne writes a single chained row via its own transaction, exactly
	// like the collector's flushBatch does. Returns the assigned id.
	appendOne := func() {
		ctx := context.Background()
		tx, err := store.BeginTx(ctx)
		require.NoError(t, err)
		tail, err := tx.TailHash(ctx)
		require.NoError(t, err)
		ua := &UserActivity{
			Ts:         time.Now().UnixMilli(),
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
		require.NoError(t, tx.Commit())
	}

	const writers = 6
	const appendsEach = 8
	const gcTickers = 2

	stopC := make(chan struct{})
	var writeWG sync.WaitGroup
	var gcWG sync.WaitGroup

	// Writers: each does its own BeginTx→Append→Commit in a tight loop.
	var writeErrs atomic.Int32
	for w := 0; w < writers; w++ {
		writeWG.Add(1)
		go func() {
			defer writeWG.Done()
			for i := 0; i < appendsEach; i++ {
				select {
				case <-stopC:
					return
				default:
				}
				// appendOne uses require inside, which would fail the whole
				// test on error — wrap to count and continue instead so the
				// race window stays open for the full load.
				func() {
					defer func() {
						if r := recover(); r != nil {
							writeErrs.Add(1)
						}
					}()
					appendOne()
				}()
			}
		}()
	}

	// GC tickers: run Tick concurrently with the writers for the whole load.
	var gcErrs atomic.Int32
	for g := 0; g < gcTickers; g++ {
		gcWG.Add(1)
		go func() {
			defer gcWG.Done()
			for {
				select {
				case <-stopC:
					return
				default:
				}
				if _, err := gc.Tick(context.Background()); err != nil {
					gcErrs.Add(1)
				}
				// Yield to maximize interleaving with concurrent appends.
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Wait for all writers to finish their fixed append count, then stop GC.
	writeWG.Wait()
	close(stopC)
	gcWG.Wait()

	require.Equal(t, int32(0), writeErrs.Load(), "concurrent appends must not error")
	require.Equal(t, int32(0), gcErrs.Load(), "gc ticks must not error under concurrent writes")

	// The core assertion: after concurrent GC + writes, the chain is intact.
	// Before the C1/C2 fix this would intermittently fail with a non-zero
	// BrokenID because a row whose self_hash fed the next row's prev_hash was
	// pruned after the checkpoint was anchored to a stale tail.
	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID,
		"chain must be intact after concurrent GC + writes (C1/C2 regression): %s", result.Reason)
}
