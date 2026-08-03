package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
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

func TestFetchMediaBytes_BoundsSDKResponseBeforeSDKBuffering(t *testing.T) {
	t.Parallel()

	resourceBody := &infiniteMediaReadCloser{}
	client := lark.NewClient("media-test-app", "media-test-secret",
		lark.WithHttpClient(newMediaBoundedHTTPClient(mediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"tenant_access_token":"test-token","expire":7200}`)),
					Request:    req,
				}, nil
			case "/open-apis/im/v1/messages/message-id/resources/resource-key":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
					Body:       resourceBody,
					Request:    req,
				}, nil
			default:
				return nil, fmt.Errorf("unexpected SDK request path %q", req.URL.Path)
			}
		}))),
	)
	adapter := newTestAdapter(t)
	adapter.larkClient = client

	data, _, err := adapter.fetchMediaBytes(context.Background(), &MediaInfo{
		Type:      "file",
		Key:       "resource-key",
		MessageID: "message-id",
	})

	require.ErrorIs(t, err, ErrMediaTooLarge)
	require.Nil(t, data)
	require.Equal(t, mediaMaxSize+1, resourceBody.bytesRead)
	require.True(t, resourceBody.closed)
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

type infiniteMediaReadCloser struct {
	infiniteMediaReader
	closed bool
}

func (r *infiniteMediaReadCloser) Close() error {
	r.closed = true
	return nil
}

type mediaHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f mediaHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (r *infiniteMediaReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	r.bytesRead += len(p)
	return len(p), nil
}
