package gateway

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTurnStopFence_ClaimOnce_Concurrent verifies that concurrent Claim calls
// for the same session/run admit exactly one winner — the gateway stop path
// must call StopCurrentTurn at most once no matter how many stops race.
func TestTurnStopFence_ClaimOnce_Concurrent(t *testing.T) {
	f := turnStopFence{}

	const claimants = 16
	wins := make(chan bool, claimants)
	var wg sync.WaitGroup
	for i := 0; i < claimants; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wins <- f.Claim("session-1", "run-1")
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
		require.True(t, f.Claim("s", "r"), "first claim admitted")
		require.False(t, f.Claim("s", "r"), "duplicate claim rejected")
	})

	t.Run("rollback allows retry", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r"), "first claim admitted")
		f.Rollback("s", "r")
		require.True(t, f.Claim("s", "r"), "retry after rollback admitted")
	})

	t.Run("new run overwrites stale claim", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1"), "old run claim admitted")
		require.True(t, f.Claim("s", "run-2"), "replaced worker run admitted (overwrites stale)")
		require.False(t, f.Claim("s", "run-2"), "new run claim now held")
	})

	t.Run("old run rollback does not clear new run", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1"))
		require.True(t, f.Claim("s", "run-2"))
		f.Rollback("s", "run-1")
		require.False(t, f.Claim("s", "run-2"), "rollback of an older run must not clear the newer run")
	})

	t.Run("begin turn clears same session and run", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "r"))
		f.BeginTurn("s", "r")
		require.True(t, f.Claim("s", "r"), "next turn can stop again")
	})

	t.Run("begin turn of another session does not clear", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s1", "r"))
		require.True(t, f.Claim("s2", "r"))
		f.BeginTurn("s1", "r")
		require.True(t, f.Claim("s1", "r"), "cleared session admits again")
		require.False(t, f.Claim("s2", "r"), "other session claim untouched")
	})

	t.Run("begin turn of another run does not clear", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("s", "run-1"))
		f.BeginTurn("s", "run-2")
		require.False(t, f.Claim("s", "run-1"), "different run must not clear the held claim")
	})

	t.Run("sessions are independent", func(t *testing.T) {
		f := turnStopFence{}
		require.True(t, f.Claim("a", "r"))
		require.True(t, f.Claim("b", "r"), "different session claims independently")
		require.False(t, f.Claim("a", "r"))
		require.False(t, f.Claim("b", "r"))
	})
}
