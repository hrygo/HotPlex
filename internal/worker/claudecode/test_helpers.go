package claudecode

import (
	"log/slog"
	"os"
)

// newTestLogger creates a logger for testing.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}
