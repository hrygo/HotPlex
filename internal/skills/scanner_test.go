package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanRootIsShallowAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	require.NoError(t, os.MkdirAll(first, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(first, "SKILL.md"), []byte("---\nname: first\ndescription: first\n---\n"), 0o644))
	second := filepath.Join(root, "second")
	require.NoError(t, os.MkdirAll(filepath.Join(second, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(second, "nested", "SKILL.md"), []byte("---\nname: nested\n---\n"), 0o644))
	linked := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(first, linked))
	linkedFile := filepath.Join(second, "linked.md")
	require.NoError(t, os.Symlink(filepath.Join(first, "SKILL.md"), linkedFile))

	got, err := ScanRoot(root, SourceGlobal, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "first", got[0].Name)
}
