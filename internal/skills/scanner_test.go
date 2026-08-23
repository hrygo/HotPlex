package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestScanRootContextHonorsCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := ScanRootContext(ctx, root, SourceGlobal, false)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestScanRootBoundsMetadataAndEntryCount(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	longName := strings.Repeat("n", maxScanNameRunes+1)
	require.NoError(t, os.MkdirAll(filepath.Join(root, longName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, longName, "SKILL.md"), []byte("---\nname: "+longName+"\ndescription: ignored\n---\n"), 0o644))
	longDescDir := filepath.Join(root, "bounded")
	require.NoError(t, os.MkdirAll(longDescDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(longDescDir, "SKILL.md"), []byte("---\nname: bounded\ndescription: "+strings.Repeat("x", maxScanDescriptionRunes+100)+"\n---\n"), 0o644))

	got, err := ScanRoot(root, SourceGlobal, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "bounded", got[0].Name)
	require.LessOrEqual(t, len([]rune(got[0].Description)), maxScanDescriptionRunes)

	tooMany := t.TempDir()
	for i := 0; i <= maxScanRootEntries; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tooMany, strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('0'+i/26))+".md"), []byte("---\nname: item\n---\n"), 0o644))
	}
	_, err = ScanRoot(tooMany, SourceGlobal, false)
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
}
