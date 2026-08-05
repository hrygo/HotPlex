package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeFileClient is a SlackAPI fake whose GetFileContext writes exactly
// `actual` bytes in deterministic chunks. The declared metadata Size is
// deliberately ignored so tests can exercise the lying-metadata path.
type fakeFileClient struct {
	SlackAPI
	actual int
}

func (f *fakeFileClient) GetFileContext(_ context.Context, _ string, w io.Writer) error {
	const chunk = 64 * 1024
	payload := bytes.Repeat([]byte{0xAB}, chunk)
	remaining := f.actual
	for remaining > 0 {
		n := chunk
		if remaining < n {
			n = remaining
		}
		if _, err := w.Write(payload[:n]); err != nil {
			return err
		}
		remaining -= n
	}
	return nil
}

// withMediaPrefix runs fn with the package global MediaPathPrefix temporarily
// pointed at a fresh temp dir, restoring the original value afterwards. These
// tests must NOT be t.Parallel() because they mutate a package global.
func withMediaPrefix(t *testing.T, fn func()) {
	t.Helper()
	orig := MediaPathPrefix
	MediaPathPrefix = t.TempDir()
	t.Cleanup(func() { MediaPathPrefix = orig })
	fn()
}

// assertNoMediaTrace verifies that a failed media write left neither the final
// path nor any .hotplex-media-* temp file behind.
func assertNoMediaTrace(t *testing.T, dir, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("final path %s must not exist after failure (stat err: %v)", path, err)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return // nothing was created at all
	}
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), ".hotplex-media-"),
			"temp file %q left behind in %s", e.Name(), dir)
	}
}

// TestDownloadMediaBytes_EnforcesActualTenMiBLimit proves the 10 MiB limit is
// enforced on the actual bytes written, not just on the declared Size metadata.
func TestDownloadMediaBytes_EnforcesActualTenMiBLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		declared  int
		actual    int
		wantError bool
	}{
		{name: "exact limit", declared: 10 * 1024 * 1024, actual: 10 * 1024 * 1024},
		{name: "one byte over", declared: 10*1024*1024 + 1, actual: 10*1024*1024 + 1, wantError: true},
		{name: "lying metadata", declared: 1, actual: 10*1024*1024 + 1, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newTestAdapter(t)
			a.client = &fakeFileClient{actual: tc.actual}
			m := &MediaInfo{
				Type:        "image",
				FileID:      "F_LIMIT",
				MimeType:    "image/png",
				Size:        tc.declared,
				DownloadURL: "https://files.slack.com/limit",
			}
			data, err := a.downloadMediaBytes(context.Background(), m)
			if tc.wantError {
				require.Error(t, err)
				require.True(t, errors.Is(err, ErrMediaTooLarge), "want ErrMediaTooLarge, got: %v", err)
				require.Nil(t, data)
				return
			}
			require.NoError(t, err)
			require.Len(t, data, tc.actual, "exactly 10 MiB must be accepted")
		})
	}
}

// TestDownloadMedia_EnforcesActualTenMiBLimit proves the file-backed download
// path enforces the actual byte limit and commits atomically: an over-limit
// download leaves neither the final path nor temp files behind.
func TestDownloadMedia_EnforcesActualTenMiBLimit(t *testing.T) {
	// Not t.Parallel(): temporarily replaces the package global MediaPathPrefix.
	withMediaPrefix(t, func() {
		mediaDir := filepath.Join(MediaPathPrefix, "images")
		cases := []struct {
			name      string
			fileID    string
			declared  int
			actual    int
			wantError bool
		}{
			{name: "exact limit", fileID: "F_OK", declared: 10 * 1024 * 1024, actual: 10 * 1024 * 1024},
			{name: "one byte over", fileID: "F_OVER", declared: 10*1024*1024 + 1, actual: 10*1024*1024 + 1, wantError: true},
			{name: "lying metadata", fileID: "F_LIE", declared: 1, actual: 10*1024*1024 + 1, wantError: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				a := newTestAdapter(t)
				a.client = &fakeFileClient{actual: tc.actual}
				m := &MediaInfo{
					Type:        "image",
					FileID:      tc.fileID,
					MimeType:    "image/png",
					Size:        tc.declared,
					DownloadURL: "https://files.slack.com/limit",
				}
				path := filepath.Join(mediaDir, fmt.Sprintf("image_%s.png", tc.fileID))
				_, err := a.downloadMedia(context.Background(), m)
				if tc.wantError {
					require.Error(t, err)
					require.True(t, errors.Is(err, ErrMediaTooLarge), "want ErrMediaTooLarge, got: %v", err)
					assertNoMediaTrace(t, mediaDir, path)
					return
				}
				require.NoError(t, err)
				info, err := os.Stat(path)
				require.NoError(t, err, "committed file must exist at final path")
				require.Equal(t, int64(tc.actual), info.Size())
			})
		}
	})
}

// TestSaveMediaBytes_PrivatePermissionsAndAtomicCleanup verifies the media dir
// and file are private and the content is intact. On Windows only the content
// assertions run; POSIX mode bits are asserted on Unix.
func TestSaveMediaBytes_PrivatePermissionsAndAtomicCleanup(t *testing.T) {
	// Not t.Parallel(): temporarily replaces the package global MediaPathPrefix.
	withMediaPrefix(t, func() {
		a := newTestAdapter(t)
		data := bytes.Repeat([]byte{0xCD}, 512)
		m := &MediaInfo{
			Type:     "audio",
			FileID:   "F_PRIV",
			MimeType: "audio/opus",
			Size:     len(data),
		}

		path, err := a.saveMediaBytes(m, data)
		require.NoError(t, err)

		dir := filepath.Dir(path)
		dirInfo, err := os.Stat(dir)
		require.NoError(t, err)
		fileInfo, err := os.Stat(path)
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, data, got, "saved content must match")

		if runtime.GOOS == "windows" {
			return // POSIX mode bits are not meaningful on Windows; content verified above
		}
		require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "media dir must be private")
		require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm(), "media file must be private")
	})
}

// TestMediaFilePath_RejectsTraversal verifies the media path builder rejects
// types and FileIDs that could escape MediaPathPrefix, and that every accepted
// path stays inside the prefix.
func TestMediaFilePath_RejectsTraversal(t *testing.T) {
	// Not t.Parallel(): temporarily replaces the package global MediaPathPrefix.
	withMediaPrefix(t, func() {
		base, err := filepath.Abs(MediaPathPrefix)
		require.NoError(t, err)

		cases := []struct {
			name   string
			info   *MediaInfo
			wantOK bool
		}{
			{name: "traversal file id", info: &MediaInfo{Type: "image", FileID: "../../escape", MimeType: "image/png"}},
			{name: "empty file id", info: &MediaInfo{Type: "image", FileID: "", MimeType: "image/png"}},
			{name: "dot file id", info: &MediaInfo{Type: "image", FileID: ".", MimeType: "image/png"}},
			{name: "dotdot file id", info: &MediaInfo{Type: "image", FileID: "..", MimeType: "image/png"}},
			{name: "unknown type", info: &MediaInfo{Type: "avatar", FileID: "F1", MimeType: "image/png"}},
			{name: "valid id stays inside prefix", info: &MediaInfo{Type: "image", FileID: "F1", MimeType: "image/png"}, wantOK: true},
			{name: "unknown mime falls back to bin", info: &MediaInfo{Type: "file", FileID: "F2", MimeType: "application/x-unknown"}, wantOK: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				dir, path, err := mediaFilePath(tc.info)
				if !tc.wantOK {
					require.Error(t, err, "mediaFilePath must reject %q", tc.info.FileID)
					return
				}
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(dir, base), "dir must stay under MediaPathPrefix: %s", dir)
				abs, err := filepath.Abs(path)
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(abs, base+string(filepath.Separator)),
					"final path must stay under MediaPathPrefix: %s", abs)
			})
		}

		// Unknown MIME types use .bin, never the FileID as extension.
		_, path, err := mediaFilePath(&MediaInfo{Type: "document", FileID: "F_BIN", MimeType: "application/x-unknown"})
		require.NoError(t, err)
		require.Equal(t, filepath.Join(base, "documents", "document_F_BIN.bin"), path)
	})
}

// TestSaveMediaBytes_OverLimitLeavesNoTrace proves the byte-array save path
// rejects over-limit payloads before any disk write.
func TestSaveMediaBytes_OverLimitLeavesNoTrace(t *testing.T) {
	// Not t.Parallel(): temporarily replaces the package global MediaPathPrefix.
	withMediaPrefix(t, func() {
		a := newTestAdapter(t)
		m := &MediaInfo{
			Type:     "document",
			FileID:   "F_OVER",
			MimeType: "application/pdf",
			Size:     10*1024*1024 + 1,
		}

		_, err := a.saveMediaBytes(m, make([]byte, 10*1024*1024+1))
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrMediaTooLarge), "want ErrMediaTooLarge, got: %v", err)

		// The len check precedes any disk writes; nothing may have been created.
		dir := filepath.Join(MediaPathPrefix, "documents")
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			return
		}
		require.NoError(t, err)
		require.Empty(t, entries, "no file may be left behind")
	})
}
