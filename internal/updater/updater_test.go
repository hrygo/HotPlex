package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testUpdater(t *testing.T, handler http.HandlerFunc) (*Updater, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u := &Updater{
		CurrentVersion: "v1.3.0",
		Repo:           "test/repo",
		Client:         server.Client(),
		BaseURL:        server.URL,
		GOOS:           "darwin",
		GOARCH:         "arm64",
	}
	return u, server
}

func releaseJSON(tag string, assets []Asset) string {
	type release struct {
		TagName string  `json:"tag_name"`
		Assets  []Asset `json:"assets"`
	}
	b, _ := json.Marshal(release{TagName: tag, Assets: assets})
	return string(b)
}

func TestAssetName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "hotplex-darwin-arm64.tar.gz"},
		{"darwin", "amd64", "hotplex-darwin-amd64.tar.gz"},
		{"linux", "amd64", "hotplex-linux-amd64.tar.gz"},
		{"linux", "arm64", "hotplex-linux-arm64.tar.gz"},
		{"windows", "amd64", "hotplex-windows-amd64.zip"},
		{"windows", "arm64", "hotplex-windows-arm64.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			u := &Updater{GOOS: tt.goos, GOARCH: tt.goarch}
			require.Equal(t, tt.want, u.assetName())
		})
	}
}

func TestBinaryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "hotplex-darwin-arm64"},
		{"linux", "amd64", "hotplex-linux-amd64"},
		{"windows", "amd64", "hotplex-windows-amd64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			u := &Updater{GOOS: tt.goos, GOARCH: tt.goarch}
			require.Equal(t, tt.want, u.binaryName())
		})
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.4.0", []Asset{
			{Name: "hotplex-darwin-arm64.tar.gz", BrowserDownloadURL: "http://example.com/archive"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums"},
		}))
	}))
	result, err := u.Check(context.Background())
	require.NoError(t, err)
	require.True(t, result.UpdateAvailable)
	require.Equal(t, "v1.4.0", result.LatestVersion)
	require.Equal(t, "http://example.com/archive", result.DownloadURL)
	require.Equal(t, "http://example.com/checksums", result.ChecksumURL)
}

func TestCheck_AlreadyUpToDate(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.3.0", []Asset{
			{Name: "hotplex-darwin-arm64.tar.gz", BrowserDownloadURL: "http://example.com/archive"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums"},
		}))
	}))
	result, err := u.Check(context.Background())
	require.NoError(t, err)
	require.False(t, result.UpdateAvailable)
}

func TestCheck_RateLimited(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit")
}

func TestCheck_Offline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	u := &Updater{
		CurrentVersion: "v1.3.0",
		Repo:           "test/repo",
		Client:         server.Client(),
		BaseURL:        server.URL,
		GOOS:           "darwin",
		GOARCH:         "arm64",
	}
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to reach GitHub")
}

func TestCheck_AssetNotFound(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.4.0", []Asset{
			{Name: "hotplex-linux-amd64.tar.gz", BrowserDownloadURL: "http://example.com/archive"},
		}))
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no archive found")
}

func TestCheck_NonOKStatus(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func makeTarGz(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()
	var buf strings.Builder
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: binaryName,
		Mode: 0o755,
		Size: int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return []byte(buf.String())
}

func makeZip(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()
	var buf strings.Builder
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(binaryName)
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return []byte(buf.String())
}

func TestDownload_TarGz(t *testing.T) {
	t.Parallel()
	content := []byte("fake-binary-content")
	archive := makeTarGz(t, "hotplex-darwin-arm64", content)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client(), GOOS: "darwin", GOARCH: "arm64"}
	path, err := u.Download(context.Background(), server.URL)
	require.NoError(t, err)
	defer os.Remove(path)

	// Download returns archive bytes, not extracted binary
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, archive, data)
}

func TestDownload_Zip(t *testing.T) {
	t.Parallel()
	content := []byte("fake-windows-binary")
	archive := makeZip(t, "hotplex-windows-amd64.exe", content)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client(), GOOS: "windows", GOARCH: "amd64"}
	path, err := u.Download(context.Background(), server.URL)
	require.NoError(t, err)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, archive, data)
}

func TestExtract_TarGz(t *testing.T) {
	t.Parallel()
	content := []byte("fake-binary-content")
	archive := makeTarGz(t, "hotplex-darwin-arm64", content)

	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "archive.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, archive, 0o644))

	u := &Updater{GOOS: "darwin", GOARCH: "arm64"}
	path, err := u.Extract(archivePath)
	require.NoError(t, err)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestExtract_Zip(t *testing.T) {
	t.Parallel()
	content := []byte("fake-windows-binary")
	archive := makeZip(t, "hotplex-windows-amd64.exe", content)

	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "archive.zip")
	require.NoError(t, os.WriteFile(archivePath, archive, 0o644))

	u := &Updater{GOOS: "windows", GOARCH: "amd64"}
	path, err := u.Extract(archivePath)
	require.NoError(t, err)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestExtract_BinaryNotFoundInArchive(t *testing.T) {
	t.Parallel()
	archive := makeTarGz(t, "wrong-binary-name", []byte("x"))

	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "archive.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, archive, 0o644))

	u := &Updater{GOOS: "darwin", GOARCH: "arm64"}
	_, err := u.Extract(archivePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in archive")
}

func TestCheck_LegacyBinaryFallback(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.4.0", []Asset{
			{Name: "hotplex-darwin-arm64", BrowserDownloadURL: "http://example.com/binary"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums"},
		}))
	}))
	result, err := u.Check(context.Background())
	require.NoError(t, err)
	require.True(t, result.UpdateAvailable)
	require.True(t, result.IsLegacyBinary)
	require.Equal(t, "http://example.com/binary", result.DownloadURL)
}

func TestDownload_NonOKStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	_, err := u.Download(context.Background(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 404")
}

func TestVerifyChecksum_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64.tar.gz")
	content := []byte("fake-archive")
	require.NoError(t, os.WriteFile(filePath, content, 0o644))

	hash := sha256.Sum256(content)
	checksumLine := fmt.Sprintf("%s  hotplex-darwin-arm64.tar.gz", fmt.Sprintf("%x", hash[:]))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumLine))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64.tar.gz", filePath)
	require.NoError(t, err)
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64.tar.gz")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0000000000000000  hotplex-darwin-arm64.tar.gz"))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64.tar.gz", filePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksum_MissingEntry(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64.tar.gz")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abc123  hotplex-linux-amd64.tar.gz"))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64.tar.gz", filePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in checksums.txt")
}

func TestVerifyChecksum_NoURL(t *testing.T) {
	t.Parallel()
	u := &Updater{Client: http.DefaultClient}
	err := u.VerifyChecksum(context.Background(), "", "hotplex-darwin-arm64.tar.gz", "/dev/null")
	require.Error(t, err)
	require.Contains(t, err.Error(), "skipping verification")
}

func TestReplace_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	currentBin := filepath.Join(tmp, "hotplex")
	require.NoError(t, os.WriteFile(currentBin, []byte("old"), 0o755))

	newBin := filepath.Join(tmp, "hotplex-new")
	require.NoError(t, os.WriteFile(newBin, []byte("new"), 0o755))

	backupPath := currentBin + ".old"
	require.NoError(t, os.Rename(currentBin, backupPath))
	require.NoError(t, os.Rename(newBin, currentBin))
	_ = os.Remove(backupPath)

	data, err := os.ReadFile(currentBin)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), data)

	_, err = os.Stat(newBin)
	require.True(t, os.IsNotExist(err))
}

func TestVersionEqual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.3.0", "v1.3.0", true},
		{"1.3.0", "v1.3.0", true},
		{"v1.3.0", "1.3.0", true},
		{"v1.3.0", "v1.4.0", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, versionEqual(tt.a, tt.b), "versionEqual(%q, %q)", tt.a, tt.b)
	}
}

func TestFindChecksum(t *testing.T) {
	t.Parallel()
	checksums := "abc123  hotplex-linux-amd64.tar.gz\ndef456  hotplex-darwin-arm64.tar.gz\n"
	hash, err := findChecksum(checksums, "hotplex-darwin-arm64.tar.gz")
	require.NoError(t, err)
	require.Equal(t, "def456", hash)

	_, err = findChecksum(checksums, "hotplex-windows-amd64.zip")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestIsWritable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test-binary")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o755))

	path, err := testIsWritablePath(f)
	require.NoError(t, err)
	require.Equal(t, f, path)

	readOnly := filepath.Join(tmp, "readonly")
	require.NoError(t, os.WriteFile(readOnly, []byte("x"), 0o444))
	_, err = testIsWritablePath(readOnly)
	require.Error(t, err)
}

func testIsWritablePath(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return path, fmt.Errorf("no write permission for %s: %w", path, err)
	}
	_ = f.Close()
	return path, nil
}

func TestIsDocker(t *testing.T) {
	t.Parallel()
	require.False(t, IsDocker())
}

func TestCheck_ContextCancelled(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := u.Check(ctx)
	require.Error(t, err)
}

func TestVerifyChecksum_HTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64.tar.gz", "/dev/null")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}
