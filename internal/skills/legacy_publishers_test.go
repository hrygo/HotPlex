package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyFlatManualPublishersRemoved(t *testing.T) {
	t.Parallel()

	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))

	for _, relativePath := range []string{
		"internal/cron/skill.go",
		"internal/cron/cron-skill-manual.md",
		"internal/messaging/phrases/skill.go",
		"internal/messaging/phrases/phrases.md",
		"internal/dbutil/skill.go",
		"internal/dbutil/db-stats-skill-manual.md",
	} {
		require.NoFileExists(t, filepath.Join(repoRoot, relativePath))
	}

	for _, relativePath := range []string{
		"internal/cron/cron.go",
		"cmd/hotplex/messaging_init.go",
		"cmd/hotplex/gateway_run.go",
	} {
		contents, err := os.ReadFile(filepath.Join(repoRoot, relativePath))
		require.NoError(t, err)
		for _, legacyCall := range []string{
			"ReleaseSkillManual",
			"releaseDBStatsManual",
			"SkillManual()",
		} {
			require.NotContains(t, string(contents), legacyCall, "%s contains legacy call %q", relativePath, legacyCall)
		}
	}
}
