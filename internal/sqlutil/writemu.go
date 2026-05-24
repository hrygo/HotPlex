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

// WithLock acquires the write mutex, calls fn, and releases it.
// If m is nil, fn is called without locking (e.g. CLI standalone mode).
func (m *WriteMu) WithLock(fn func() error) error {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
	}
	return fn()
}
