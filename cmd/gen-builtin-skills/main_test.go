package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateBuiltinSkillsUsesCanonicalBytesWithoutRegistry(t *testing.T) {
	t.Parallel()

	canonicalRoot := t.TempDir()
	files := map[string][]string{
		"hotplex-cli": {
			"SKILL.md",
			"references/cli-surface.generated.md",
			"references/cron.md",
			"references/diagnostics.md",
			"references/slack.md",
		},
		"hotplex-operator": {
			"SKILL.md",
			"references/admin-audit.md",
			"references/configuration.md",
			"references/install-update.md",
			"references/service-lifecycle.md",
		},
	}
	for packageName, packageFiles := range files {
		for _, relativePath := range packageFiles {
			filePath := filepath.Join(canonicalRoot, packageName, filepath.FromSlash(relativePath))
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
			content := []byte("reference for " + packageName + "/" + relativePath + "\n")
			if relativePath == "SKILL.md" {
				content = []byte("---\nname: " + packageName + "\ndescription: test\ncompatibility: test\n---\n")
			}
			require.NoError(t, os.WriteFile(filePath, content, 0o644))
		}
	}

	manifestOutput := filepath.Join(t.TempDir(), "manifest.generated.go")
	mirrorRoot := filepath.Join(t.TempDir(), "hotplex-cli")
	err := generate(generatorConfig{
		canonicalRoot:  canonicalRoot,
		manifestOutput: manifestOutput,
		mirrorRoot:     mirrorRoot,
	})
	require.NoError(t, err)

	manifest, err := os.ReadFile(manifestOutput)
	require.NoError(t, err)
	require.Contains(t, string(manifest), "hotplex-cli")
	require.Contains(t, string(manifest), "hotplex-operator")
	require.FileExists(t, filepath.Join(mirrorRoot, "SKILL.md"))
	require.FileExists(t, filepath.Join(mirrorRoot, "references", "cron.md"))
}

func TestGenerateBuiltinSkillsRejectsIncompleteCanonicalTree(t *testing.T) {
	t.Parallel()

	canonicalRoot := t.TempDir()
	for _, packageName := range []string{"hotplex-cli", "hotplex-operator"} {
		require.NoError(t, os.MkdirAll(filepath.Join(canonicalRoot, packageName), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(canonicalRoot, packageName, "SKILL.md"),
			[]byte("---\nname: "+packageName+"\ndescription: test\ncompatibility: test\n---\n"),
			0o644,
		))
	}

	err := generate(generatorConfig{
		canonicalRoot:  canonicalRoot,
		manifestOutput: filepath.Join(t.TempDir(), "manifest.generated.go"),
		mirrorRoot:     filepath.Join(t.TempDir(), "hotplex-cli"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "want 5")
}
