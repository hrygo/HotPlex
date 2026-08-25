package gateway

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTurnStopFence_ClaimOnce_Concurrent verifies that concurrent Claim calls
// for the same session/run/execution admit exactly one winner — the gateway
// stop path must call StopCurrentTurn at most once no matter how many stops
// race.
func TestTurnStopFence_ClaimOnce_Concurrent(t *testing.T) {
	f := turnStopFence{}

	const claimants = 16
	wins := make(chan bool, claimants)
	var wg sync.WaitGroup
	for i := 0; i < claimants; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wins <- f.Claim("session-1", "run-1", "exec-1")
		}()
	}
	wg.Wait()
	close(wins)

	admitted := 0
	for w := range wins {
		if w {
			admitted++
		}
	}
	require.Equal(t, 1, admitted, "exactly one concurrent Claim must win")
}

// TestTurnStopFence_Sequential cases exercise the claim lifecycle without
// concurrency: duplicate claim, retry after rollback, stale-run rollback, and
// BeginTurn scoping (same session, same run only). Each subtest owns a fresh
// fence so a held claim cannot leak across cases.
func TestTurnStopFence_Sequential(t *testing.T) {
	t.Parallel()

	t.Run("duplicate claim rejected", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "e"), "first claim admitted")
		require.True(t, f.Matches("s", "r", "e"), "held claim remains observable after its run detaches")
		require.False(t, f.Matches("s", "r", "next"), "a later execution must not match a stale stop claim")
		require.True(t, f.HasAny("s"), "identity-free fallback can still observe a retained claim")
		require.False(t, f.Claim("s", "r", "e"), "duplicate claim rejected")
	})

	t.Run("rollback allows retry", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "e"), "first claim admitted")
		f.Rollback("s", "r", "e")
		require.True(t, f.Claim("s", "r", "e"), "retry after rollback admitted")
	})

	t.Run("new run overwrites stale claim", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1", "e"), "old run claim admitted")
		require.True(t, f.Claim("s", "run-2", "e"), "replaced worker run admitted (overwrites stale)")
		require.False(t, f.Claim("s", "run-2", "e"), "new run claim now held")
	})

	t.Run("new execution overwrites stale claim", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "exec-1"), "first turn claim admitted")
		require.True(t, f.Claim("s", "r", "exec-2"), "next turn on same run admitted (overwrites stale)")
		require.False(t, f.Claim("s", "r", "exec-2"), "next turn claim now held")
	})

	t.Run("old run rollback does not clear new run", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1", "e"))
		require.True(t, f.Claim("s", "run-2", "e"))
		f.Rollback("s", "run-1", "e")
		require.False(t, f.Claim("s", "run-2", "e"), "rollback of an older run must not clear the newer run")
	})

	t.Run("old execution rollback does not clear new execution", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "exec-1"))
		require.True(t, f.Claim("s", "r", "exec-2"))
		f.Rollback("s", "r", "exec-1")
		require.False(t, f.Claim("s", "r", "exec-2"), "rollback of an older execution must not clear the newer one")
	})

	t.Run("begin turn clears same session run and execution", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "e"))
		f.BeginTurn("s", "r", "e")
		require.True(t, f.Claim("s", "r", "e"), "next turn can stop again")
	})

	t.Run("begin turn of another session does not clear", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s1", "r", "e"))
		require.True(t, f.Claim("s2", "r", "e"))
		f.BeginTurn("s1", "r", "e")
		require.True(t, f.Claim("s1", "r", "e"), "cleared session admits again")
		require.False(t, f.Claim("s2", "r", "e"), "other session claim untouched")
	})

	t.Run("begin turn of another run does not clear", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1", "e"))
		f.BeginTurn("s", "run-2", "e")
		require.False(t, f.Claim("s", "run-1", "e"), "different run must not clear the held claim")
	})

	// Regression for the in-flight-stop race (finding #3): a new turn's input
	// carries a NEW execution ID, so its BeginTurn must NOT delete the claim a
	// still-running stop just acquired for the OLD execution — otherwise a
	// double-clicked/retried stop would be re-admitted and stop twice.
	t.Run("begin turn of new execution does not clear in-flight stop claim", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "exec-1"), "stop claims turn 1")
		f.BeginTurn("s", "r", "exec-2") // racing input starts turn 2
		require.False(t, f.Claim("s", "r", "exec-1"), "duplicate stop for turn 1 still rejected")
		require.True(t, f.Claim("s", "r", "exec-2"), "stop for turn 2 admitted under its own key")
	})

	t.Run("sessions are independent", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("a", "r", "e"))
		require.True(t, f.Claim("b", "r", "e"), "different session claims independently")
		require.False(t, f.Claim("a", "r", "e"))
		require.False(t, f.Claim("b", "r", "e"))
	})

	t.Run("delete releases claim", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r", "e"))
		f.Delete("s")
		require.True(t, f.Claim("s", "r", "e"), "deleted session claim released")
	})

	t.Run("delete of other session does not clear", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s1", "r", "e"))
		require.True(t, f.Claim("s2", "r", "e"))
		f.Delete("s1")
		require.True(t, f.Claim("s1", "r", "e"), "deleted session admits again")
		require.False(t, f.Claim("s2", "r", "e"), "other session claim untouched")
	})
}
