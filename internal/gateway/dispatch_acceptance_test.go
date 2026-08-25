package gateway

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunAcceptedDispatch_RecoversDispatchPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		acceptBefore bool
	}{
		{name: "before acceptance"},
		{name: "after acceptance", acceptBefore: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var releases atomic.Int32
			accepted, err := runAcceptedDispatch(func(markAccepted func()) error {
				if tt.acceptBefore {
					markAccepted()
				}
				panic("injected dispatch panic")
			}, nil, func() {
				releases.Add(1)
			})

			require.Equal(t, tt.acceptBefore, accepted)
			require.ErrorIs(t, err, errWorkerDispatchPanic)
			require.EqualValues(t, 1, releases.Load())
		})
	}
}

func TestRunAcceptedDispatch_FinalizesAcceptanceBeforeRelease(t *testing.T) {
	t.Parallel()

	var finalized atomic.Bool
	accepted, err := runAcceptedDispatch(func(markAccepted func()) error {
		markAccepted()
		return nil
	}, func() {
		finalized.Store(true)
	}, func() {
		require.True(t, finalized.Load(), "acceptance side effects must precede admission release")
	})

	require.True(t, accepted)
	require.NoError(t, err)
}

func TestRunAcceptedDispatch_FastSuccessFinalizesAcceptance(t *testing.T) {
	t.Parallel()

	var finalized atomic.Bool
	accepted, err := runAcceptedDispatch(func(func()) error {
		return nil
	}, func() {
		finalized.Store(true)
	}, func() {
		require.True(t, finalized.Load(), "successful return is completed delivery")
	})

	require.True(t, accepted)
	require.NoError(t, err)
}

func TestRunAcceptedDispatch_AcceptancePanicStillReleases(t *testing.T) {
	t.Parallel()

	var releases atomic.Int32
	require.Panics(t, func() {
		_, _ = runAcceptedDispatch(func(markAccepted func()) error {
			markAccepted()
			return nil
		}, func() {
			panic("injected acceptance finalization panic")
		}, func() {
			releases.Add(1)
		})
	})
	require.EqualValues(t, 1, releases.Load())
}
