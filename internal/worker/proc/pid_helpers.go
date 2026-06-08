package proc

import "log/slog"

// TrackPID writes (key, pgid) to the global PID tracker. No-op if tracker is
// nil or key is empty. On write error, logs a warning prefixed with component.
func TrackPID(key string, pgid int, log *slog.Logger, component string) {
	t := GlobalTracker()
	if t == nil || key == "" {
		return
	}
	if err := t.Write(key, pgid); err != nil {
		log.Warn(component+": pidfile write", "key", key, "err", err)
	}
}

// UntrackPID removes the PID file entry for key. No-op if tracker is nil or
// key is empty.
func UntrackPID(key string) {
	t := GlobalTracker()
	if t == nil || key == "" {
		return
	}
	_ = t.Remove(key)
}
