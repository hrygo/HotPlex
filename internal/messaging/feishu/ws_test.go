package feishu

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

// TestNewEventHandlerInjectsSlogLogger guards against the os.Stdout hijack
// regression (PR #828 review P1). The Lark dispatcher's default logger writes
// to os.Stdout; the adapter must inject its own SlogLogger instead of
// redirecting the process-global stdout, which raced with concurrent goroutines.
func TestNewEventHandlerInjectsSlogLogger(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{Log: logger},
		},
	}

	stdoutBefore := os.Stdout
	d := a.newEventHandler()
	require.Equal(t, stdoutBefore, os.Stdout, "os.Stdout must not be mutated (race source)")

	_, ok := d.Config.Logger.(SlogLogger)
	require.True(t, ok, "dispatcher Config.Logger must be SlogLogger, got %T", d.Config.Logger)
}
