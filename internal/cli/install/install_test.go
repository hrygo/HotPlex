package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExeName(t *testing.T) {
	t.Parallel()
	name := ExeName()
	if runtime.GOOS == "windows" {
		require.Equal(t, "hotplex.exe", name)
	} else {
		require.Equal(t, "hotplex", name)
	}
}

func TestDefaultInstallDir(t *testing.T) {
	t.Parallel()
	dir, err := DefaultInstallDir()
	require.NoError(t, err)
	require.NotEmpty(t, dir)

	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		require.Equal(t, filepath.Join(home, ".hotplex", "bin"), dir)
	} else {
		require.Equal(t, filepath.Join(home, ".local", "bin"), dir)
	}
}

func TestResolveSourceBinary(t *testing.T) {
	t.Parallel()
	bin, err := ResolveSourceBinary()
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(bin), "should be absolute: %s", bin)

	// Should resolve to an existing file
	fi, err := os.Stat(bin)
	require.NoError(t, err)
	require.False(t, fi.IsDir())
}

func TestCopyBinary(t *testing.T) {
	t.Parallel()

	src := t.TempDir() + "/src"
	dst := t.TempDir() + "/dst"

	// Write source content
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o644))

	require.NoError(t, CopyBinary(src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))

	// Verify executable permission
	fi, err := os.Stat(dst)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&0o111, "should be executable")
}

func TestCopyBinaryAtomic(t *testing.T) {
	t.Parallel()

	src := t.TempDir() + "/src"
	dstDir := t.TempDir()
	dst := dstDir + "/dst"

	require.NoError(t, os.WriteFile(src, []byte("content"), 0o644))

	// No leftover temp files on success
	require.NoError(t, CopyBinary(src, dst))
	entries, _ := os.ReadDir(dstDir)
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hotplex-install-") {
			count++
		}
	}
	require.Equal(t, 0, count, "no temp files should remain")
}

func TestSameContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	a := dir + "/a"
	b := dir + "/b"
	c := dir + "/c"

	require.NoError(t, os.WriteFile(a, []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(c, []byte("different"), 0o644))

	require.True(t, SameContent(a, b), "identical files")
	require.False(t, SameContent(a, c), "different content")
	require.False(t, SameContent(a, dir+"/nonexistent"), "missing file")
}

func TestIsInPATH(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Not in PATH initially
	require.False(t, IsInPATH(dir))

	// Add to PATH and verify
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
	require.True(t, IsInPATH(dir))
}

func TestIsInPATHTailingSlash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	// PATH has trailing slash, dir does not
	os.Setenv("PATH", dir+"/"+string(os.PathListSeparator)+orig)
	require.True(t, IsInPATH(dir), "should match despite trailing slash")
}

func TestIsInPATHNotInPath(t *testing.T) {
	t.Parallel()

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	// Empty PATH
	os.Setenv("PATH", "")
	require.False(t, IsInPATH("/some/random/dir"))
}

func TestShellEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"/usr/local/bin", "/usr/local/bin"},
		{"has space", "'has space'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"foo;bar", `'foo;bar'`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shellEscape(tt.input))
		})
	}
}
