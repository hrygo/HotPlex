package contracttest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestHarness_BasicTurnAndCleanup drives one input envelope through a full
// F-C harness (temp SQLite store + session manager + hub + bridge + handler +
// WorkerProbe) and asserts the clean lifecycle: the probe's Input is called
// exactly once, the hub observes the mapper events (delta + done), child
// prompts are not involved, the store file is closed and removable, and the
// harness teardown leaves no goroutines or FDs behind.
func TestHarness_BasicTurnAndCleanup(t *testing.T) {
	// Register the file-removability check BEFORE NewHarness so t.Cleanup's
	// LIFO order runs the harness teardown (which closes the store) first and
	// this check last. By then t.TempDir's own cleanup has removed the whole
	// directory — the file being gone proves the store released its handle.
	var dbPath string
	t.Cleanup(func() {
		if dbPath == "" {
			return
		}
		require.NoFileExists(t, dbPath, "sqlite file should be closed and removable after harness teardown")
		require.NoDirExists(t, filepath.Dir(dbPath))
	})

	h := NewHarness(t, e2econtract.PlatformFeishu, e2econtract.WorkerProfiles()[0])
	dbPath = h.DBPath()

	env := events.NewEnvelope(aep.NewID(), h.SessionID(), 1, events.Input, map[string]any{"content": "hello"})
	require.NoError(t, h.Handler.Handle(context.Background(), env))

	require.Equal(t, 1, h.Worker().InputCalls(), "harness worker probe should receive exactly one input")

	evs := h.WaitForKinds(t, events.MessageDelta, events.Done)
	require.NotEmpty(t, evs, "hub should have observed the mapper events")

	h.AssertSingleTerminal(t, "")
}
