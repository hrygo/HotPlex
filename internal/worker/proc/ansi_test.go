package proc

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureHandler records slog records for assertion without touching real sinks.
type captureHandler struct {
	entries []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.entries = append(h.entries, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestStripANSI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain line unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "SGR color codes stripped",
			in:   "\x1b[31mERROR\x1b[0m something",
			want: "ERROR something",
		},
		{
			name: "SGR dim code stripped",
			in:   "\x1b[2m2026-08-07T15:10:47Z\x1b[0m",
			want: "2026-08-07T15:10:47Z",
		},
		{
			name: "multi-param SGR stripped",
			in:   "\x1b[1;31mbold red\x1b[0m",
			want: "bold red",
		},
		{
			name: "rmcp sample line fully cleaned",
			in:   "\x1b[2m2026-08-07T15:10:47.054946Z\x1b[0m \x1b[31mERROR\x1b[0m \x1b[2mrmcp::transport::worker\x1b[0m\x1b[2m:\x1b[0m worker quit with fatal: Transport channel closed",
			want: "2026-08-07T15:10:47.054946Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed",
		},
		{
			name: "OSC title sequence stripped",
			in:   "\x1b]0;terminal title\x07body",
			want: "body",
		},
		{
			name: "stray ESC before regular char keeps the char",
			in:   "a\x1bb",
			want: "ab",
		},
		{
			name: "trailing lone ESC dropped",
			in:   "end\x1b",
			want: "end",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripANSI(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestManager_drainStderr_StripsANSI(t *testing.T) {
	t.Parallel()

	t.Run("ANSI line logged without escape sequences", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		require.NoError(t, err)
		rc := &mockReadCloser{Reader: r}

		_, _ = w.WriteString("\x1b[31mERROR\x1b[0m worker quit with fatal\n")
		_ = w.Close()

		cap := &captureHandler{}
		m := New(Opts{Logger: slog.New(cap)})
		m.drainStderr(rc)

		require.Len(t, cap.entries, 1, "exactly one stderr line should be logged")
		entry := cap.entries[0]
		require.Equal(t, "proc: stderr", entry.Message)
		var stderrVal string
		entry.Attrs(func(a slog.Attr) bool {
			if a.Key == "stderr" {
				stderrVal = a.Value.String()
			}
			return true
		})
		require.Equal(t, "ERROR worker quit with fatal", stderrVal)
		require.False(t, strings.ContainsRune(stderrVal, 0x1b), "no ESC byte should survive into the log")
	})

	t.Run("ANSI-wrapped [ERROR] marker still maps to Error level", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		require.NoError(t, err)
		rc := &mockReadCloser{Reader: r}

		_, _ = w.WriteString("\x1b[31m[ERROR]\x1b[0m transport failed\n")
		_ = w.Close()

		cap := &captureHandler{}
		m := New(Opts{Logger: slog.New(cap)})
		m.drainStderr(rc)

		require.Len(t, cap.entries, 1)
		require.Equal(t, slog.LevelError, cap.entries[0].Level,
			"level marker hidden behind ANSI codes must still be recognized after stripping")
	})

	t.Run("plain lines unaffected", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		require.NoError(t, err)
		rc := &mockReadCloser{Reader: r}

		_, _ = w.WriteString("plain line\n")
		_ = w.Close()

		cap := &captureHandler{}
		m := New(Opts{Logger: slog.New(cap)})
		m.drainStderr(rc)

		require.Len(t, cap.entries, 1)
		var stderrVal string
		cap.entries[0].Attrs(func(a slog.Attr) bool {
			if a.Key == "stderr" {
				stderrVal = a.Value.String()
			}
			return true
		})
		require.Equal(t, "plain line", stderrVal)
	})
}
