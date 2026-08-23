package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestClaudeCatalogUsesOnlyExactNativeRoot(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	project := filepath.Join(home, "project")
	require.NoError(t, writeClaudeSkill(filepath.Join(home, ".claude", "skills", "oracle-dba"), "oracle-dba", "Inspect Oracle"))
	require.NoError(t, writeClaudeSkill(filepath.Join(home, ".hotplex", "skills", "not-native"), "not-native", "Inventory only"))
	require.NoError(t, writeClaudeSkill(filepath.Join(project, ".claude", "skills", "project-only"), "project-only", "Project only"))
	require.NoError(t, writeClaudeSkill(filepath.Join(home, ".agents", "skills", "managed"), "managed", "Managed only"))

	w := claudeCatalogWorkerForHome(home)
	got, err := w.ListInvokableSkills(context.Background(), project)
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{{
		Name:        "oracle-dba",
		Description: "Inspect Oracle",
		Path:        filepath.Join(home, ".claude", "skills", "oracle-dba", "SKILL.md"),
	}}, got)
}

func TestClaudeCatalogReturnsNativePathAndMetadataSorted(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, ".claude", "skills")
	require.NoError(t, writeClaudeSkill(filepath.Join(root, "zeta"), "zeta", "Zed"))
	require.NoError(t, writeClaudeSkill(filepath.Join(root, "alpha"), "alpha", "Alpha"))

	got, err := claudeCatalogWorkerForHome(home).ListInvokableSkills(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []string{"alpha", "zeta"}, []string{got[0].Name, got[1].Name})
	for _, descriptor := range got {
		require.True(t, filepath.IsAbs(descriptor.Path))
		require.FileExists(t, descriptor.Path)
	}
}

func TestClaudeCatalogSkipsSymlinkEntries(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, ".claude", "skills")
	realDir := filepath.Join(root, "real")
	require.NoError(t, writeClaudeSkill(realDir, "real", "Real"))
	require.NoError(t, os.Symlink(realDir, filepath.Join(root, "linked-dir")))
	require.NoError(t, os.Symlink(filepath.Join(realDir, "SKILL.md"), filepath.Join(root, "linked.md")))

	got, err := claudeCatalogWorkerForHome(home).ListInvokableSkills(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{{
		Name:        "real",
		Description: "Real",
		Path:        filepath.Join(realDir, "SKILL.md"),
	}}, got)
}

func TestClaudeCatalogMissingRootReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	got, err := claudeCatalogWorkerForHome(t.TempDir()).ListInvokableSkills(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestClaudeCatalogRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	require.NoError(t, writeClaudeSkill(filepath.Join(home, ".claude", "skills", "oracle-dba"), "oracle-dba", "Inspect Oracle"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := claudeCatalogWorkerForHome(home).ListInvokableSkills(ctx, "")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, got)
}

func TestClaudeCatalogBoundsMetadataAndSkipsInvalidFrontmatter(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, ".claude", "skills")
	require.NoError(t, writeClaudeSkill(filepath.Join(root, "valid"), "valid", strings.Repeat("x", 2048)))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "invalid"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "invalid", "SKILL.md"), []byte("# missing frontmatter\n"), 0o644))

	got, err := claudeCatalogWorkerForHome(home).ListInvokableSkills(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "valid", got[0].Name)
	require.LessOrEqual(t, len([]rune(got[0].Description)), 1024)
}

func TestClaudeCatalogPropagatesHomeResolverError(t *testing.T) {
	t.Parallel()

	want := errors.New("home unavailable")
	w := New()
	w.userHomeDir = func() (string, error) { return "", want }

	got, err := w.ListInvokableSkills(context.Background(), "")
	require.ErrorIs(t, err, want)
	require.Empty(t, got)
}

func claudeCatalogWorkerForHome(home string) *Worker {
	w := New()
	w.userHomeDir = func() (string, error) { return home, nil }
	return w
}

func writeClaudeSkill(dir, name, description string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
	return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func TestClaudeCatalogDescriptorOrderIsStable(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, ".claude", "skills")
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		require.NoError(t, writeClaudeSkill(filepath.Join(root, name), name, name))
	}

	got, err := claudeCatalogWorkerForHome(home).ListInvokableSkills(context.Background(), "")
	require.NoError(t, err)
	names := make([]string, 0, len(got))
	for _, descriptor := range got {
		names = append(names, descriptor.Name)
	}
	require.True(t, sort.StringsAreSorted(names))
}
