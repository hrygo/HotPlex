//go:build windows

package proc

import (
	"context"
	"log/slog"
	"time"
)

// ForceKillTree is a no-op on Windows where Job Objects handle tree cleanup.
func ForceKillTree(pgid int, log *slog.Logger) {}

// GracefulTerminateTree is a no-op on Windows where Job Objects handle tree cleanup.
func GracefulTerminateTree(ctx context.Context, pgid int, gracePeriod time.Duration, log *slog.Logger) {
}
