package agentconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFile_CreatesDirAndWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := WriteFile(dir, "slack", "U12345", "SOUL.md", "hello world", 100)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "slack", "U12345", "SOUL.md"))
	require.NoError(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestWriteFile_RejectsOversized(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	content := make([]byte, 101)
	for i := range content {
		content[i] = 'x'
	}

	err := WriteFile(dir, "slack", "U12345", "SOUL.md", string(content), 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds limit")
}

func TestWriteFile_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := WriteFile(dir, "slack", "../evil", "SOUL.md", "pwned", 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path traversal not allowed")
}

func TestWriteFile_WritesCanonicalToolsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := WriteFile(dir, "slack", "U12345", "TOOLS.md", "prefer native tools", 100)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "slack", "U12345", "TOOLS.md"))
}

func TestWriteFile_RejectsLegacySkillsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := WriteFile(dir, "slack", "U12345", "SKILLS.md", "legacy", 100)
	require.ErrorIs(t, err, ErrUnknownConfigFile)
	require.NoFileExists(t, filepath.Join(dir, "slack", "U12345", "SKILLS.md"))
}

func TestResolvedSource_BotLevel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create bot-level file
	botDir := filepath.Join(dir, "slack", "U12345")
	require.NoError(t, os.MkdirAll(botDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("bot"), 0o644))

	src := ResolvedSource(dir, "slack", "U12345", "SOUL.md")
	require.Equal(t, "bot", src)
}

func TestResolvedSource_PlatformLevel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create platform-level file only (no bot file)
	platDir := filepath.Join(dir, "slack")
	require.NoError(t, os.MkdirAll(platDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(platDir, "SOUL.md"), []byte("platform"), 0o644))

	src := ResolvedSource(dir, "slack", "U12345", "SOUL.md")
	require.Equal(t, "platform", src)
}

func TestResolvedSource_GlobalLevel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create only global file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("global"), 0o644))

	src := ResolvedSource(dir, "slack", "U12345", "SOUL.md")
	require.Equal(t, "global", src)
}

func TestResolvedSource_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := ResolvedSource(dir, "slack", "U12345", "SOUL.md")
	require.Equal(t, "", src)
}

func TestResolvedSource_CanonicalToolsIgnoresLegacyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	botDir := filepath.Join(dir, "slack", "U12345")
	require.NoError(t, os.MkdirAll(botDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(botDir, "SKILLS.md"), []byte("legacy"), 0o644))

	src := ResolvedSource(dir, "slack", "U12345", "TOOLS.md")
	require.Empty(t, src)
}

func TestResolvedSource_LegacyDefaultDirectoryIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	legacyDir := filepath.Join(dir, "slack", "default")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "TOOLS.md"), []byte("legacy"), 0o644))

	source, resolvedFile := ResolvedLocation(dir, "slack", "", "TOOLS.md")
	require.Empty(t, source)
	require.Empty(t, resolvedFile)
}
