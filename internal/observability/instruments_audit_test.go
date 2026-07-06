package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAuditInstruments verifies all 5 audit metric accessors return non-nil
// instruments. No Init() call is needed — Meter() falls back to a noop meter
// before Init, so instrument creation succeeds without side effects.
func TestAuditInstruments(t *testing.T) {
	t.Parallel()

	t.Run("AuditEvents", func(t *testing.T) {
		m := AuditEvents()
		require.NotNil(t, m)
	})

	t.Run("AuditChainBreaks", func(t *testing.T) {
		m := AuditChainBreaks()
		require.NotNil(t, m)
	})

	t.Run("AuditSpill", func(t *testing.T) {
		m := AuditSpill()
		require.NotNil(t, m)
	})

	t.Run("AuditWriteFailures", func(t *testing.T) {
		m := AuditWriteFailures()
		require.NotNil(t, m)
	})

	t.Run("AuditSinkFailures", func(t *testing.T) {
		m := AuditSinkFailures()
		require.NotNil(t, m)
	})
}
