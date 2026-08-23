package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateBuiltinSkillsUsesCanonicalBytesWithoutRegistry(t *testing.T) {
	t.Parallel()

	canonicalRoot := writeCanonicalFixture(t)

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
	require.NoError(t, compareDirectoryFiles(
		filepath.Join(canonicalRoot, "hotplex-cli"),
		mirrorRoot,
	))

	extraPath := filepath.Join(mirrorRoot, "extra.md")
	require.NoError(t, os.WriteFile(extraPath, []byte("extra\n"), 0o644))
	require.Error(t, compareDirectoryFiles(
		filepath.Join(canonicalRoot, "hotplex-cli"),
		mirrorRoot,
	))
	require.NoError(t, generate(generatorConfig{
		canonicalRoot:  canonicalRoot,
		manifestOutput: manifestOutput,
		mirrorRoot:     mirrorRoot,
	}))
	require.NoError(t, compareDirectoryFiles(
		filepath.Join(canonicalRoot, "hotplex-cli"),
		mirrorRoot,
	))
}

func TestGeneratedPackageVersionTracksCanonicalContent(t *testing.T) {
	t.Parallel()

	canonicalRoot := writeCanonicalFixture(t)
	manifestOutput := filepath.Join(t.TempDir(), "manifest.generated.go")
	mirrorRoot := filepath.Join(t.TempDir(), "hotplex-cli")
	config := generatorConfig{
		canonicalRoot:  canonicalRoot,
		manifestOutput: manifestOutput,
		mirrorRoot:     mirrorRoot,
	}
	require.NoError(t, generate(config))
	first, err := generatedPackageVersion(manifestOutput, "hotplex-cli")
	require.NoError(t, err)
	require.Regexp(t, "^v1-[0-9a-f]{64}$", first)

	contentPath := filepath.Join(canonicalRoot, "hotplex-cli", "references", "cron.md")
	changedContent := mustReadFile(t, contentPath)
	changedContent[0] ^= 1
	require.NoError(t, os.WriteFile(contentPath, changedContent, 0o644))
	require.NoError(t, generate(config))
	second, err := generatedPackageVersion(manifestOutput, "hotplex-cli")
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	require.NoError(t, generate(config))
	third, err := generatedPackageVersion(manifestOutput, "hotplex-cli")
	require.NoError(t, err)
	require.Equal(t, second, third)
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

func TestMirrorPackagePreservesFixedBackup(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), []byte("old\n"), 0o644))
	fixedBackup := targetRoot + ".backup"
	require.NoError(t, os.WriteFile(fixedBackup, []byte("keep\n"), 0o644))

	require.NoError(t, mirrorPackage(sourceRoot, targetRoot))
	got, err := os.ReadFile(fixedBackup)
	require.NoError(t, err)
	require.Equal(t, []byte("keep\n"), got)
	require.Equal(t, []byte("new\n"), mustReadFile(t, filepath.Join(targetRoot, "SKILL.md")))
}

func TestMirrorPackageRollsBackWhenPromoteRenameFails(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), []byte("old\n"), 0o644))

	original := fsOps
	t.Cleanup(func() { fsOps = original })
	renameCalls := 0
	fsOps.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("injected promote failure")
		}
		return os.Rename(oldPath, newPath)
	}

	err := mirrorPackage(sourceRoot, targetRoot)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected promote failure")
	require.Equal(t, []byte("old\n"), mustReadFile(t, filepath.Join(targetRoot, "SKILL.md")))
}

func TestMirrorPackageReportsBackupCleanupFailure(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), []byte("old\n"), 0o644))

	original := fsOps
	t.Cleanup(func() { fsOps = original })
	fsOps.removeAll = func(path string) error {
		if strings.Contains(filepath.Base(path), ".hotplex-backup-") {
			return errors.New("injected backup cleanup failure")
		}
		return os.RemoveAll(path)
	}

	err := mirrorPackage(sourceRoot, targetRoot)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected backup cleanup failure")
	require.Equal(t, []byte("old\n"), mustReadFile(t, filepath.Join(targetRoot, "SKILL.md")))
}

func TestWriteAtomicRollsBackExistingTargetWhenPromoteRenameFails(t *testing.T) {
	targetRoot := filepath.Join(t.TempDir(), "manifest.generated.go")
	require.NoError(t, os.WriteFile(targetRoot, []byte("old\n"), 0o644))

	original := fsOps
	t.Cleanup(func() { fsOps = original })
	renameCalls := 0
	fsOps.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("injected file promote failure")
		}
		return os.Rename(oldPath, newPath)
	}

	err := writeAtomic(targetRoot, []byte("new\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected file promote failure")
	require.Equal(t, []byte("old\n"), mustReadFile(t, targetRoot))
}

func writeCanonicalFixture(t *testing.T) string {
	t.Helper()
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
	return canonicalRoot
}

func generatedPackageVersion(path, packageName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile("(?s)Name:\\s+\"" + regexp.QuoteMeta(packageName) + "\".*?Version:\\s+\"([^\"]+)\"")
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		return "", fmt.Errorf("package %q version not found", packageName)
	}
	return string(match[1]), nil
}

func compareDirectoryFiles(sourceRoot, targetRoot string) error {
	sourceFiles, err := directoryFiles(sourceRoot)
	if err != nil {
		return err
	}
	targetFiles, err := directoryFiles(targetRoot)
	if err != nil {
		return err
	}
	if len(sourceFiles) != len(targetFiles) {
		return fmt.Errorf("file count mismatch: source=%d target=%d", len(sourceFiles), len(targetFiles))
	}
	for relativePath, sourceData := range sourceFiles {
		targetData, ok := targetFiles[relativePath]
		if !ok {
			return fmt.Errorf("target missing %s", relativePath)
		}
		if !bytes.Equal(sourceData, targetData) {
			return fmt.Errorf("file %s differs", relativePath)
		}
	}
	for relativePath := range targetFiles {
		if _, ok := sourceFiles[relativePath]; !ok {
			return fmt.Errorf("target has extra %s", relativePath)
		}
	}
	return nil
}

func directoryFiles(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relativePath)] = data
		return nil
	})
	return files, err
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
