package sqlutil

import "sync"

// WriteMu serializes SQLite write operations across all stores sharing
// the same database file. In WAL mode, reads proceed concurrently;
// only writes are serialized to prevent SQLITE_BUSY errors.
type WriteMu struct {
	mu sync.Mutex
}

// NewWriteMu creates a new write serializer.
func NewWriteMu() *WriteMu { return &WriteMu{} }

// Lock acquires the write mutex.
func (m *WriteMu) Lock() { m.mu.Lock() }

// Unlock releases the write mutex.
func (m *WriteMu) Unlock() { m.mu.Unlock() }
