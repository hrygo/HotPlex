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
			}, func() {
				releases.Add(1)
			})

			require.Equal(t, tt.acceptBefore, accepted)
			require.ErrorIs(t, err, errWorkerDispatchPanic)
			require.EqualValues(t, 1, releases.Load())
		})
	}
}
