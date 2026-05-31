package acp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TraceWriter logs all JSON-RPC messages to a JSONL trace file for debugging.
// Enabled via acp.debug: true in config.yaml.
//
// File location: {dir}/acp-trace-{sessionID}.jsonl
// Each line: {"ts":"...","dir":"→|←","msg":{...}}
// Rotation: when file exceeds maxSize, renamed to .1 and a new file is created.
type TraceWriter struct {
	mu      sync.Mutex
	file    *os.File
	enc     *json.Encoder
	path    string
	maxSize int64
}

// NewTraceWriter creates a trace writer that appends to a JSONL file.
// Returns nil if the file cannot be created (logs a warning and continues).
func NewTraceWriter(dir, sessionID string) (*TraceWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("acp trace: create dir: %w", err)
	}
	path := filepath.Join(dir, "acp-trace-"+sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("acp trace: open file: %w", err)
	}
	enc := json.NewEncoder(f)
	return &TraceWriter{
		file:    f,
		enc:     enc,
		path:    path,
		maxSize: 50 * 1024 * 1024, // 50 MB
	}, nil
}

// Log writes a single trace entry. Safe to call on a nil TraceWriter (no-op).
func (tw *TraceWriter) Log(direction string, msg any) {
	if tw == nil {
		return
	}
	entry := map[string]any{
		"ts":  time.Now().Format(time.RFC3339Nano),
		"dir": direction,
		"msg": msg,
	}
	tw.mu.Lock()
	_ = tw.enc.Encode(entry)
	tw.mu.Unlock()
}

// Close flushes and closes the trace file.
func (tw *TraceWriter) Close() error {
	if tw == nil {
		return nil
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.file.Close()
}

// Rotate checks the file size and rotates if it exceeds maxSize.
// The old file is renamed with a .1 suffix and a new file is created.
func (tw *TraceWriter) Rotate(maxSize int64) error {
	if tw == nil {
		return nil
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()

	info, err := tw.file.Stat()
	if err != nil {
		return fmt.Errorf("acp trace: stat: %w", err)
	}
	if info.Size() < maxSize {
		return nil
	}

	// Close current file.
	if err := tw.file.Close(); err != nil {
		return fmt.Errorf("acp trace: close for rotate: %w", err)
	}

	// Rename old file.
	rotated := tw.path + ".1"
	_ = os.Remove(rotated) // ignore error if .1 doesn't exist
	if err := os.Rename(tw.path, rotated); err != nil {
		slog.Warn("acp trace: failed to rename for rotation", "error", err)
	}

	// Open new file.
	f, err := os.OpenFile(tw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("acp trace: reopen after rotate: %w", err)
	}
	tw.file = f
	tw.enc = json.NewEncoder(f)
	return nil
}

// Path returns the current trace file path (for diagnostics).
func (tw *TraceWriter) Path() string {
	if tw == nil {
		return ""
	}
	return tw.path
}
