package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWarnDeprecatedSuffixFiles_NormalizesLegacySkillsTargetToTools(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILLS.slack.md"), []byte("legacy"), 0o644))

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	warnDeprecatedSuffixFiles(dir, log)

	require.Contains(t, logs.String(), "slack/TOOLS.md")
	require.NotContains(t, logs.String(), "slack/SKILLS.md")
}
