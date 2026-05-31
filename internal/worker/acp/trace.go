package acp

import (
	"encoding/json"
	"fmt"
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
// Also checks file size and triggers rotation when the configured max is exceeded.
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
	if tw.enc == nil {
		tw.mu.Unlock()
		return
	}
	_ = tw.enc.Encode(entry)
	// Check rotation after write (best-effort, ignore error).
	if f, err := tw.file.Stat(); err == nil && f.Size() >= tw.maxSize {
		tw.rotateLocked()
	}
	tw.mu.Unlock()
}

// rotateLocked performs file rotation. Caller must hold tw.mu.
// On failure, tw.file is set to nil so subsequent Log() calls degrade gracefully.
func (tw *TraceWriter) rotateLocked() {
	if err := tw.file.Close(); err != nil {
		return
	}
	rotated := tw.path + ".1"
	_ = os.Remove(rotated)
	if err := os.Rename(tw.path, rotated); err != nil {
		tw.file = nil
		tw.enc = nil
		return
	}
	f, err := os.OpenFile(tw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		tw.file = nil
		tw.enc = nil
		return
	}
	tw.file = f
	tw.enc = json.NewEncoder(f)
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
// Safe to call on nil TraceWriter. Rotation is also triggered automatically by Log().
func (tw *TraceWriter) Rotate(maxSize int64) error {
	if tw == nil {
		return nil
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.file == nil {
		return nil
	}
	info, err := tw.file.Stat()
	if err != nil {
		return fmt.Errorf("acp trace: stat: %w", err)
	}
	if info.Size() < maxSize {
		return nil
	}
	tw.rotateLocked()
	return nil
}

// Path returns the current trace file path (for diagnostics).
func (tw *TraceWriter) Path() string {
	if tw == nil {
		return ""
	}
	return tw.path
}
