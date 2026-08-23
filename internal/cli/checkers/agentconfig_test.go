package checkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestAgentConfigDirChecker(t *testing.T) {
	t.Parallel()

	t.Run("valid structure passes", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "SOUL.md"), []byte("soul"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "AGENTS.md"), []byte("agents"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "TOOLS.md"), []byte("tool guidance"), 0o644))

		c := agentConfigDirChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})

	t.Run("unsupported global markdown is unrecognized", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "SKILLS.md"), []byte("unsupported"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "custom.md"), []byte("unsupported"), 0o644))

		d := (agentConfigDirChecker{dir: cfgDir}).Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "SKILLS.md")
		require.Contains(t, d.Message, "custom.md")
		require.Contains(t, d.Message, "unrecognized")
		require.NotContains(t, d.Message, "deprecated")
		require.NotContains(t, d.FixHint, "backup")
	})

	t.Run("unsupported platform markdown is unrecognized", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "SKILLS.md"), []byte("unsupported"), 0o644))

		d := (agentConfigDirChecker{dir: cfgDir}).Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, filepath.Join("slack", "SKILLS.md"))
		require.Contains(t, d.Message, "unrecognized")
		require.NotContains(t, d.Message, "deprecated")
		require.NotContains(t, d.FixHint, "backup")
	})

	t.Run("present empty tools file warns about explicit clear", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack", "helper"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "helper", "TOOLS.md"), []byte(" \n"), 0o644))

		d := (agentConfigDirChecker{dir: cfgDir}).Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, filepath.Join("slack", "helper", "TOOLS.md"))
		require.Contains(t, d.Message, "explicit clear")
	})

	t.Run("frontmatter-only tools file warns about explicit clear", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(cfgDir, "TOOLS.md"),
			[]byte("---\nversion: 1\n---\n"),
			0o644,
		))

		d := (agentConfigDirChecker{dir: cfgDir}).Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "TOOLS.md")
		require.Contains(t, d.Message, "explicit clear")
	})

	t.Run("valid with bot subdirectory", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack", "U12345"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "U12345", "SOUL.md"), []byte("bot soul"), 0o644))

		c := agentConfigDirChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})

	t.Run("unrecognized md file in platform dir warns", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "custom.md"), []byte("custom"), 0o644))

		c := agentConfigDirChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "custom.md")
	})

	t.Run("ignored files are allowed", func(t *testing.T) {
		t.Parallel()
		cfgDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", ".gitkeep"), []byte(""), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "README.md"), []byte("readme"), 0o644))

		c := agentConfigDirChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})

	t.Run("nonexistent directory warns", func(t *testing.T) {
		t.Parallel()
		c := agentConfigDirChecker{dir: filepath.Join(t.TempDir(), "nonexistent")}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "Cannot read")
	})
}
