//go:build windows

package proc

import (
	"log/slog"
)

// ForceKillTree is a no-op on Windows where Job Objects handle tree cleanup.
func ForceKillTree(pgid int, log *slog.Logger) {}
