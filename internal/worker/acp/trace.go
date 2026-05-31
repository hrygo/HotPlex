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
	mu           sync.Mutex
	file         *os.File
	path         string
	maxSize      int64
	writtenBytes int64
}

// NewTraceWriter creates a trace writer that appends to a JSONL file.
func NewTraceWriter(dir, sessionID string) (*TraceWriter, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("acp trace: create dir: %w", err)
	}
	path := filepath.Join(dir, "acp-trace-"+sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("acp trace: open file: %w", err)
	}
	return &TraceWriter{
		file:    f,
		path:    path,
		maxSize: 50 * 1024 * 1024, // 50 MB
	}, nil
}

// Log writes a single trace entry. Safe to call on a nil TraceWriter (no-op).
// Uses a byte counter under mutex to track size, avoiding Stat() syscall on every call.
func (tw *TraceWriter) Log(direction string, msg any) {
	if tw == nil {
		return
	}
	// Marshal outside the lock to reduce contention.
	line, err := json.Marshal(map[string]any{
		"ts":  time.Now().Format(time.RFC3339Nano),
		"dir": direction,
		"msg": msg,
	})
	if err != nil {
		return
	}
	line = append(line, '\n')

	tw.mu.Lock()
	if tw.file == nil {
		tw.mu.Unlock()
		return
	}
	_, _ = tw.file.Write(line)
	tw.writtenBytes += int64(len(line))

	// Check rotation using byte counter.
	if tw.writtenBytes >= tw.maxSize {
		tw.writtenBytes = 0
		tw.rotateLocked()
	}
	tw.mu.Unlock()
}

// rotateLocked performs file rotation. Caller must hold tw.mu.
// On failure, tw.file is set to nil so subsequent Log() calls degrade gracefully.
func (tw *TraceWriter) rotateLocked() {
	if err := tw.file.Close(); err != nil {
		tw.file = nil
		return
	}
	rotated := tw.path + ".1"
	_ = os.Remove(rotated)
	if err := os.Rename(tw.path, rotated); err != nil {
		tw.file = nil
		return
	}
	f, err := os.OpenFile(tw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		tw.file = nil
		return
	}
	tw.file = f
}

// Close flushes and closes the trace file.
func (tw *TraceWriter) Close() error {
	if tw == nil {
		return nil
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.file == nil {
		return nil
	}
	return tw.file.Close()
}

// Rotate forces a rotation. Safe to call on nil TraceWriter.
// Rotation is also triggered automatically by Log() when the file exceeds maxSize.
func (tw *TraceWriter) Rotate() {
	if tw == nil {
		return
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.file == nil {
		return
	}
	tw.writtenBytes = 0
	tw.rotateLocked()
}

// Path returns the current trace file path (for diagnostics).
func (tw *TraceWriter) Path() string {
	if tw == nil {
		return ""
	}
	return tw.path
}
