package admin

import (
	"sync"
	"time"
)

// logEntry represents a single log entry in the ring buffer.
type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Msg     string `json:"msg"`
	Session string `json:"session_id,omitempty"`
}

// logRingBuffer is a thread-safe ring buffer for recent log entries.
type logRingBuffer struct {
	mu   sync.Mutex
	ent  []logEntry
	head int
	n    int // total entries ever added
}

// newLogRing creates a new ring buffer with the given capacity.
func newLogRing(cap int) *logRingBuffer {
	return &logRingBuffer{ent: make([]logEntry, cap)}
}

// Add adds a new entry to the ring buffer.
func (r *logRingBuffer) Add(level, msg, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ent[r.head%len(r.ent)] = logEntry{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Level:   level,
		Msg:     msg,
		Session: sessionID,
	}
	r.head++
	r.n++
}

// Total returns the total number of entries ever added.
func (r *logRingBuffer) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// Recent returns the most recent entries up to the given limit.
func (r *logRingBuffer) Recent(limit int) []logEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return nil
	}
	size := len(r.ent)
	size = min(r.n, size)
	if limit > 0 && limit < size {
		size = limit
	}
	// start from oldest
	start := (r.head - size) % len(r.ent)
	out := make([]logEntry, 0, size)
	for i := 0; i < size; i++ {
		idx := (start + i) % len(r.ent)
		out = append(out, r.ent[idx])
	}
	return out
}

// LogCollector is the interface for accessing log entries.
type LogCollector interface {
	Recent(limit int) []logEntry
}

var LogRing = newLogRing(100)

func AddLog(level, msg, sessionID string) {
	LogRing.Add(level, msg, sessionID)
}
