package gateway

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock               { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }
func (f *fakeClock) now() time.Time          { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func TestPermissionDenyDedup_SuppressesWithinWindow(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	require.False(t, d.RegisterRequest("s1", "r1", "u1", "fp1"), "first request must not hit")
	d.RecordDeny("s1", "r1", "u1")
	require.True(t, d.RegisterRequest("s1", "r2", "u1", "fp1"), "same fp within window must hit")
}

func TestPermissionDenyDedup_MissAfterWindowExpires(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	clk.advance(61 * time.Second)
	require.False(t, d.RegisterRequest("s1", "r2", "u1", "fp1"), "expired denial must not hit")
}

func TestPermissionDenyDedup_DifferentFingerprintNoHit(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	require.False(t, d.RegisterRequest("s1", "r2", "u1", "fp2"))
}

func TestPermissionDenyDedup_DifferentOwnerNoHit(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	require.False(t, d.RegisterRequest("s1", "r2", "u2", "fp1"), "other owner must not inherit denial")
}

func TestPermissionDenyDedup_DifferentSessionNoHit(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	require.False(t, d.RegisterRequest("s2", "r2", "u1", "fp1"))
}

func TestPermissionDenyDedup_ClearSession(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	d.ClearSession("s1")
	require.False(t, d.RegisterRequest("s1", "r2", "u1", "fp1"))
}

func TestPermissionDenyDedup_RecordDenyUnknownReqNoOp(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	// RecordDeny for a reqID never registered must not panic or seed state.
	d.RecordDeny("s1", "ghost", "u1")
	require.False(t, d.RegisterRequest("s1", "r1", "u1", "fp1"))
}

func TestPermissionDenyDedup_NilReceiverSafe(t *testing.T) {
	t.Parallel()
	var d *PermissionDenyDedup
	require.False(t, d.RegisterRequest("s1", "r1", "u1", "fp1"))
	d.RecordDeny("s1", "r1", "u1") // must not panic
	d.ClearSession("s1")
}

func TestPermissionDenyDedup_AllowPathDoesNotDeny(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	// User allowed: RegisterRequest seeds reqIndex but RecordDeny is never
	// called, so a later same-fp request must not hit.
	d.RegisterRequest("s1", "r1", "u1", "fp1")
	require.False(t, d.RegisterRequest("s1", "r2", "u1", "fp1"))
}

func TestPermissionDenyDedup_SuppressedReqDoesNotLeakReqIndex(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)

	d.RegisterRequest("s1", "r1", "u1", "fp1")
	d.RecordDeny("s1", "r1", "u1")
	// Suppressed request (hit) must not register its reqID — a RecordDeny for
	// it must be a no-op (no panic, no state change).
	require.True(t, d.RegisterRequest("s1", "r2", "u1", "fp1"))
	d.RecordDeny("s1", "r2", "u1")
}

func TestPermissionDenyDedup_Concurrent(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	d := newPermissionDenyDedup(60*time.Second, clk.now)
	d.RegisterRequest("s1", "seed", "u1", "fp1")
	d.RecordDeny("s1", "seed", "u1")

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d.RegisterRequest("s1", "r"+strconv.Itoa(i), "u1", "fp1")
		}(i)
	}
	wg.Wait()
}

func TestComputeFingerprint_JSONKeyOrderStable(t *testing.T) {
	t.Parallel()
	a := ComputeFingerprint("Bash", []string{`{"command":"ls","dir":"/tmp"}`})
	b := ComputeFingerprint("Bash", []string{`{"dir":"/tmp","command":"ls"}`})
	require.Equal(t, a, b)
}

func TestComputeFingerprint_DifferentArgsDiffer(t *testing.T) {
	t.Parallel()
	require.NotEqual(t,
		ComputeFingerprint("Bash", []string{`{"command":"ls"}`}),
		ComputeFingerprint("Bash", []string{`{"command":"rm"}`}),
	)
}

func TestComputeFingerprint_DifferentToolDiffers(t *testing.T) {
	t.Parallel()
	require.NotEqual(t,
		ComputeFingerprint("Bash", []string{`{"command":"ls"}`}),
		ComputeFingerprint("Read", []string{`{"command":"ls"}`}),
	)
}

func TestComputeFingerprint_NonJSONArgsFallback(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		ComputeFingerprint("X", []string{"a", "b"}),
		ComputeFingerprint("X", []string{"a", "b"}),
	)
	// Order matters under the fallback join, so swapped args differ.
	require.NotEqual(t,
		ComputeFingerprint("X", []string{"a", "b"}),
		ComputeFingerprint("X", []string{"b", "a"}),
	)
}

func TestComputeFingerprint_EmptyArgs(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, ComputeFingerprint("X", nil))
	require.Equal(t, ComputeFingerprint("X", nil), ComputeFingerprint("X", []string{}))
}
