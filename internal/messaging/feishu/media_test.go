package feishu

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadMediaBytes_AllowsExactlyMediaMaxSize(t *testing.T) {
	t.Parallel()

	want := bytes.Repeat([]byte{'a'}, mediaMaxSize)
	data, err := readMediaBytes(bytes.NewReader(want))

	require.NoError(t, err)
	require.Equal(t, want, data)
}

func TestReadMediaBytes_RejectsMediaMaxSizePlusOne(t *testing.T) {
	t.Parallel()

	data, err := readMediaBytes(bytes.NewReader(bytes.Repeat([]byte{'a'}, mediaMaxSize+1)))

	require.ErrorIs(t, err, ErrMediaTooLarge)
	require.Nil(t, data)
}

func TestReadMediaBytes_StopsAtLimitForInfiniteReader(t *testing.T) {
	t.Parallel()

	reader := &infiniteMediaReader{}
	data, err := readMediaBytes(reader)

	require.ErrorIs(t, err, ErrMediaTooLarge)
	require.Nil(t, data)
	require.Equal(t, mediaMaxSize+1, reader.bytesRead)
}

func TestSaveMediaFile_UsesPrivatePermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}

	mediaDir := filepath.Join(t.TempDir(), "media", "images")
	path, err := saveMediaFile(mediaDir, []byte("media content"), "payload.bin")

	require.NoError(t, err)
	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	dirInfo, err := os.Stat(mediaDir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

func TestSaveMediaFile_RejectsUnsafeFilename(t *testing.T) {
	t.Parallel()

	mediaDir := filepath.Join(t.TempDir(), "media", "files")
	path, err := saveMediaFile(mediaDir, []byte("media content"), "../escaped.bin")

	require.Error(t, err)
	require.Empty(t, path)
	require.NoFileExists(t, filepath.Join(filepath.Dir(mediaDir), "escaped.bin"))
}

type infiniteMediaReader struct {
	bytesRead int
}

func (r *infiniteMediaReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	r.bytesRead += len(p)
	return len(p), nil
}
