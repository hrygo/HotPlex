package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamingTerminalFailuresInstrument(t *testing.T) {
	t.Parallel()

	// The accessor must be safe before Init: Meter() supplies a noop meter and
	// sync.Once caches the lazily-created instrument for production callers.
	require.NotNil(t, StreamingTerminalFailures())
}
