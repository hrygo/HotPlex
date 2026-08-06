package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// ErrMediaTooLarge is returned when a Slack media file exceeds the hard
// 10 MiB ingestion limit. The limit is enforced on the actual bytes written,
// not just on the declared metadata size.
var ErrMediaTooLarge = errors.New("slack: media exceeds 10 MiB")

const mediaMaxSize = 10 * 1024 * 1024 // 10 MiB hard limit

// uploadMaxSize is the outbound upload limit for locally-generated files
// (Slack's default 20 MB). It is deliberately separate from the 10 MiB
// ingestion cap: downloads and saves stay bounded, uploads keep the
// platform default.
const uploadMaxSize = 20 * 1024 * 1024 // 20 MB (Slack default)

const (
	mediaTypeImage    = "image"
	mediaTypeAudio    = "audio"
	mediaTypeVideo    = "video"
	mediaTypeDocument = "document"
	mediaTypeFile     = "file"

	audioMaxSizeBytes = 5 * 1024 * 1024 // 5 MB heuristic threshold for voice detection
)

// MediaInfo holds metadata about an attached file.
type MediaInfo struct {
	Type        string // "image", "video", "audio", "document", "file"
	FileID      string
	Name        string
	MimeType    string
	Size        int
	DownloadURL string // url_private_download
	PublicURL   string // permalink
}

// ConvertMessage converts a Slack MessageEvent into text + media info.
func (a *Adapter) ConvertMessage(msgEvent slackevents.MessageEvent) (text string, ok bool, media []*MediaInfo) {
	text = extractText(msgEvent)

	// Extract files from msgEvent.Files (if available)
	msg := msgEvent.Message
	if msg != nil && len(msg.Files) > 0 {
		media = make([]*MediaInfo, 0, len(msg.Files))
		for _, f := range msg.Files {
			if f.User == a.botID {
				continue
			}
			if f.IsExternal || f.ExternalType != "" {
				continue
			}
			media = append(media, &MediaInfo{
				Type:        fileCategory(f),
				FileID:      f.ID,
				Name:        f.Name,
				MimeType:    f.Mimetype,
				Size:        f.Size,
				DownloadURL: f.URLPrivateDownload,
				PublicURL:   f.Permalink,
			})
		}
	}

	// file_share but no text → generate placeholder
	if text == "" && len(media) > 0 {
		var parts []string
		for _, m := range media {
			switch m.Type {
			case mediaTypeImage:
				parts = append(parts, fmt.Sprintf("[user shared an image: %s]", m.Name))
			case mediaTypeAudio:
				parts = append(parts, fmt.Sprintf("[user sent a voice message: %s]", m.Name))
			default:
				parts = append(parts, fmt.Sprintf("[user shared a file: %s]", m.Name))
			}
		}
		text = strings.Join(parts, " ")
	}

	return text, text != "" || len(media) > 0, media
}

// fileCategory classifies a Slack file by its filetype.
func fileCategory(f slack.File) string {
	if f.Mode == "voice" {
		return mediaTypeAudio
	}
	if f.Filetype == "mp4" && f.Size > 0 && f.Size < audioMaxSizeBytes &&
		f.OriginalW == 0 && f.OriginalH == 0 {
		return mediaTypeAudio
	}
	switch f.Filetype {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg":
		return mediaTypeImage
	case "mp4", "mov", "avi", "webm", "flv":
		return mediaTypeVideo
	case "mp3", "wav", "ogg", "opus", "m4a":
		return mediaTypeAudio
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "csv", "md":
		return mediaTypeDocument
	default:
		return mediaTypeFile
	}
}

// boundedMediaWriter caps the bytes actually written to dst at maxBytes.
// Writes are allowed while total == maxBytes; a write that would exceed the
// cap writes only the remaining permitted bytes and then returns
// ErrMediaTooLarge. The over-limit tail is never written to dst.
type boundedMediaWriter struct {
	dst      io.Writer
	written  int64
	maxBytes int64
}

func (w *boundedMediaWriter) Write(p []byte) (int, error) {
	remaining := w.maxBytes - w.written
	if remaining <= 0 {
		return 0, ErrMediaTooLarge
	}
	if int64(len(p)) <= remaining {
		n, err := w.dst.Write(p)
		w.written += int64(n)
		return n, err
	}
	n, err := w.dst.Write(p[:int(remaining)])
	w.written += int64(n)
	if err != nil {
		return n, err
	}
	// Report the bytes actually written — the io.Writer contract is n < len(p)
	// with an error, never an over-report, even on a short write with nil err.
	return n, ErrMediaTooLarge
}

// downloadMedia downloads a file from Slack to local storage. The declared
// size is only a fast pre-check; the actual bytes written are bounded again
// by boundedMediaWriter, and the file is committed atomically via temp file +
// rename, so a failed download leaves neither the final path nor temp files.
func (a *Adapter) downloadMedia(ctx context.Context, m *MediaInfo) (string, error) {
	if m.Size > mediaMaxSize {
		return "", fmt.Errorf("file too large: %w", ErrMediaTooLarge)
	}

	dir, path, err := mediaFilePath(m)
	if err != nil {
		return "", err
	}

	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := a.writeMediaAtomic(dir, path, func(w io.Writer) error {
		return a.client.GetFileContext(downloadCtx, m.DownloadURL, &boundedMediaWriter{dst: w, maxBytes: mediaMaxSize})
	}); err != nil {
		return "", err
	}
	return path, nil
}

func (a *Adapter) downloadMediaBytes(ctx context.Context, m *MediaInfo) ([]byte, error) {
	if m.Size > mediaMaxSize {
		return nil, fmt.Errorf("file too large: %w", ErrMediaTooLarge)
	}

	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	if err := a.client.GetFileContext(downloadCtx, m.DownloadURL, &boundedMediaWriter{dst: &buf, maxBytes: mediaMaxSize}); err != nil {
		if errors.Is(err, ErrMediaTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("get file bytes: %w", err)
	}
	return buf.Bytes(), nil
}

func (a *Adapter) saveMediaBytes(m *MediaInfo, data []byte) (string, error) {
	if len(data) > mediaMaxSize {
		return "", fmt.Errorf("media too large: %w", ErrMediaTooLarge)
	}

	dir, path, err := mediaFilePath(m)
	if err != nil {
		return "", err
	}

	if err := a.writeMediaAtomic(dir, path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}); err != nil {
		return "", err
	}
	return path, nil
}

// writeMediaAtomic writes content through fn into a fresh temp file inside dir
// and atomically renames it to path. On any failure the temp file is removed;
// after commit the final file is chmodded 0o600. dir is created 0o700 and
// re-chmodded to 0o700 when it already exists.
func (a *Adapter) writeMediaAtomic(dir, path string, fn func(io.Writer) error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".hotplex-media-*")
	if err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = os.Remove(f.Name())
		}
	}()

	if err := fn(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return err
	}
	committed = true
	return nil
}

// mediaFilePath returns (dir, fullPath) for a media file on local storage.
// Type and FileID are validated so the final path can never escape
// MediaPathPrefix; unknown MIME types get a .bin extension.
func mediaFilePath(m *MediaInfo) (string, string, error) {
	switch m.Type {
	case mediaTypeImage, mediaTypeAudio, mediaTypeVideo, mediaTypeDocument, mediaTypeFile:
	default:
		return "", "", fmt.Errorf("slack: invalid media type %q", m.Type)
	}
	if m.FileID == "" || m.FileID == "." || m.FileID == ".." || filepath.Base(m.FileID) != m.FileID {
		return "", "", fmt.Errorf("slack: invalid media file id %q", m.FileID)
	}
	ext := mimeExt(m.MimeType)
	if ext == "" {
		ext = ".bin"
	}
	dir := filepath.Join(MediaPathPrefix, m.Type+"s")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s%s", m.Type, m.FileID, ext))
	return dir, path, nil
}

func mimeExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/opus":
		return ".opus"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}
