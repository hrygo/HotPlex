package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionDispatchGate_SerializesSameSession(t *testing.T) {
	t.Parallel()

	var gate sessionDispatchGate
	unlockFirst := gate.Lock("session-one")
	secondAcquired := make(chan struct{})
	go func() {
		unlockSecond := gate.Lock("session-one")
		close(secondAcquired)
		unlockSecond()
	}()

	require.Never(t, func() bool {
		select {
		case <-secondAcquired:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 5*time.Millisecond, "same-session acquisition must wait")

	unlockFirst()
	select {
	case <-secondAcquired:
	case <-time.After(time.Second):
		t.Fatal("same-session acquisition did not proceed after release")
	}
}

func TestSessionDispatchGate_AllowsDifferentStripes(t *testing.T) {
	t.Parallel()

	const firstSession = "session-one"
	secondSession := "session-two"
	for sessionDispatchStripe(firstSession) == sessionDispatchStripe(secondSession) {
		secondSession += "-next"
	}

	var gate sessionDispatchGate
	unlockFirst := gate.Lock(firstSession)
	defer unlockFirst()

	secondAcquired := make(chan struct{})
	go func() {
		unlockSecond := gate.Lock(secondSession)
		close(secondAcquired)
		unlockSecond()
	}()

	select {
	case <-secondAcquired:
	case <-time.After(time.Second):
		t.Fatal("different dispatch stripes must not block each other")
	}
}
