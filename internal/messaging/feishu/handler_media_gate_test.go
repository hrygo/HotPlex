package feishu

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
)

type handlerMediaTranscriber struct {
	calls atomic.Int32
}

func (t *handlerMediaTranscriber) Transcribe(_ context.Context, audioData []byte) (string, error) {
	t.calls.Add(1)
	if len(audioData) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	return "recognized audio", nil
}

func (t *handlerMediaTranscriber) RequiresDisk() bool { return true }

type handlerMediaHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f handlerMediaHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newHandlerMediaClient(downloads *atomic.Int32, mediaType string) *lark.Client {
	return lark.NewClient("handler-media-test-app", "handler-media-test-secret",
		lark.WithHttpClient(handlerMediaHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"tenant_access_token":"handler-media-token","expire":7200}`)),
					Request:    req,
				}, nil
			case strings.Contains(req.URL.Path, "/resources/"):
				downloads.Add(1)
				contentType := "application/octet-stream"
				if mediaType == "image" {
					contentType = "image/png"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{contentType}},
					Body:       io.NopCloser(bytes.NewReader([]byte("handler media bytes"))),
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok"}`)),
					Request:    req,
				}, nil
			}
		})),
	)
}

func newHandlerMediaEvent(messageID, messageType, content, userID, chatID string) *larkim.P2MessageReceiveV1 {
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId(userID).Build()).
		SenderType("user").
		Build()
	message := larkim.NewEventMessageBuilder().
		MessageId(messageID).
		MessageType(messageType).
		Content(content).
		ChatId(chatID).
		ChatType("p2p").
		Build()
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: message},
	}
}

func handlerMediaFiles(t *testing.T, mediaType, key string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(config.TempBaseDir(), "media", mediaType+"s", key+"*"))
	require.NoError(t, err)
	return files
}

// These tests set process-global temp directory variables, so they must not
// call t.Parallel. Each test gets a private temp root for media artifacts.
func isolateHandlerMediaTemp(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tempDir)
	}
}

func TestHandleMessage_GateRejectsImageBeforeDownloadAndRollsBackDedup(t *testing.T) {
	isolateHandlerMediaTemp(t)
	a := newTestAdapter(t)
	const (
		messageID = "handler-media-gate-blocked-image"
		mediaKey  = "handler-media-gate-blocked-image-key"
	)
	var downloads atomic.Int32
	a.larkClient = newHandlerMediaClient(&downloads, "image")
	a.Gate = messaging.NewGate("allowlist", "open", false, []string{"allowed-user"}, nil, nil)

	err := a.handleMessage(context.Background(), newHandlerMediaEvent(
		messageID, "image", `{"image_key":"`+mediaKey+`"}`, "blocked-user", "handler-media-gate-chat",
	))

	require.NoError(t, err)
	require.Zero(t, downloads.Load(), "a gate-rejected image must not be downloaded")
	require.Empty(t, handlerMediaFiles(t, "image", mediaKey), "a gate-rejected image must not be written to disk")
	require.True(t, a.Dedup.TryRecord(messageID), "a gate rejection must roll back dedup for retry")
}

func TestHandleMessage_GateRejectsAudioBeforeDownloadSTTAndDisk(t *testing.T) {
	isolateHandlerMediaTemp(t)
	a := newTestAdapter(t)
	const (
		messageID = "handler-media-gate-blocked-audio"
		mediaKey  = "handler-media-gate-blocked-audio-key"
	)
	var downloads atomic.Int32
	transcriber := &handlerMediaTranscriber{}
	a.larkClient = newHandlerMediaClient(&downloads, "audio")
	a.transcriber = transcriber
	a.Gate = messaging.NewGate("allowlist", "open", false, []string{"allowed-user"}, nil, nil)

	err := a.handleMessage(context.Background(), newHandlerMediaEvent(
		messageID, "audio", `{"file_key":"`+mediaKey+`"}`, "blocked-user", "handler-media-gate-chat",
	))

	require.NoError(t, err)
	require.Zero(t, downloads.Load(), "a gate-rejected audio must not be downloaded")
	require.Zero(t, transcriber.calls.Load(), "a gate-rejected audio must not invoke STT")
	require.Empty(t, handlerMediaFiles(t, "audio", mediaKey), "a gate-rejected audio must not be written to disk")
	require.True(t, a.Dedup.TryRecord(messageID), "a gate rejection must roll back dedup for retry")
}

func TestHandleMessage_GateAllowsImageDownloadBeforeQueue(t *testing.T) {
	isolateHandlerMediaTemp(t)
	a := newTestAdapter(t)
	const (
		messageID = "handler-media-gate-allowed-image"
		mediaKey  = "handler-media-gate-allowed-image-key"
		chatID    = "handler-media-gate-allowed-chat"
	)
	var downloads atomic.Int32
	a.larkClient = newHandlerMediaClient(&downloads, "image")
	a.Gate = messaging.NewGate("open", "open", false, nil, nil, nil)

	q := NewChatQueue(discardLogger)
	w := &chatWorker{tasks: make(chan func(context.Context) error, 1)}
	q.mu.Lock()
	q.workers[chatID] = w
	q.mu.Unlock()
	a.chatQueue = q
	t.Cleanup(q.Close)

	err := a.handleMessage(context.Background(), newHandlerMediaEvent(
		messageID, "image", `{"image_key":"`+mediaKey+`"}`, "allowed-user", chatID,
	))

	require.NoError(t, err)
	require.Equal(t, int32(1), downloads.Load(), "an allowed image must still be downloaded")
	require.Len(t, w.tasks, 1, "an allowed image must be admitted to chatQueue")
	files := handlerMediaFiles(t, "image", mediaKey)
	require.Len(t, files, 1, "an allowed image must be saved")
	data, readErr := os.ReadFile(files[0])
	require.NoError(t, readErr)
	require.Equal(t, []byte("handler media bytes"), data)
}
