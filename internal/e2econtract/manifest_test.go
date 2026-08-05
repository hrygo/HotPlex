package e2econtract

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestCombinations_ExactMatrix(t *testing.T) {
	t.Parallel()
	got := Combinations()
	require.Equal(t, []Combination{
		{ID: "F-C", Platform: PlatformFeishu, Worker: worker.TypeClaudeCode},
		{ID: "F-O", Platform: PlatformFeishu, Worker: worker.TypeOpenCodeSrv},
		{ID: "F-X", Platform: PlatformFeishu, Worker: worker.TypeCodexCLI},
		{ID: "F-A", Platform: PlatformFeishu, Worker: worker.TypeACP},
		{ID: "S-C", Platform: PlatformSlack, Worker: worker.TypeClaudeCode},
		{ID: "S-O", Platform: PlatformSlack, Worker: worker.TypeOpenCodeSrv},
		{ID: "S-X", Platform: PlatformSlack, Worker: worker.TypeCodexCLI},
		{ID: "S-A", Platform: PlatformSlack, Worker: worker.TypeACP},
		{ID: "W-C", Platform: PlatformWebChat, Worker: worker.TypeClaudeCode},
		{ID: "W-O", Platform: PlatformWebChat, Worker: worker.TypeOpenCodeSrv},
		{ID: "W-X", Platform: PlatformWebChat, Worker: worker.TypeCodexCLI},
		{ID: "W-A", Platform: PlatformWebChat, Worker: worker.TypeACP},
	}, got)
}

func TestCombinations_Unique(t *testing.T) {
	t.Parallel()
	combos := Combinations()
	require.Len(t, combos, 12, "matrix must have exactly 12 rows")

	ids := make(map[string]struct{}, len(combos))
	pairs := make(map[string]struct{}, len(combos))
	for _, c := range combos {
		id := CombinationID(c.Platform, c.Worker)
		require.Equal(t, c.ID, id, "CombinationID must round-trip the row ID")
		_, dupID := ids[id]
		require.False(t, dupID, "duplicate combination ID %q", id)
		ids[id] = struct{}{}

		key := string(c.Platform) + "/" + string(c.Worker)
		_, dupPair := pairs[key]
		require.False(t, dupPair, "duplicate platform/worker pair %q", key)
		pairs[key] = struct{}{}
	}
}

func TestWorkerProfiles_ExactCapabilities(t *testing.T) {
	t.Parallel()
	got := WorkerProfiles()
	require.Equal(t, []WorkerProfile{
		// Stop/reset/interaction are Native for all four matrix workers.
		// Resume follows the actual SupportsResume() implementations:
		// claudecode=true, codexcli=true, opencodeserver=true, acp=true.
		// Mid-turn input: Native for Claude/Codex, GatewayFallback for OCS/ACP.
		{Type: worker.TypeClaudeCode, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: Native},
		{Type: worker.TypeOpenCodeSrv, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: GatewayFallback},
		{Type: worker.TypeCodexCLI, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: Native},
		{Type: worker.TypeACP, Stop: Native, Reset: Native, Resume: Native, Interaction: Native, MidTurnInput: GatewayFallback},
	}, got)
}
