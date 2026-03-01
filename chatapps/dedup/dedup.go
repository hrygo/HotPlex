package dedup

import (
	"sync"
	"time"
)

// Deduplicator implements event deduplication using LRU cache
type Deduplicator struct {
	cache      map[string]time.Time // event_key -> timestamp
	mu         sync.RWMutex
	window     time.Duration // Deduplication window
	cleanupInt time.Duration // Cleanup interval
	done       chan struct{}
	wg         sync.WaitGroup
}

// NewDeduplicator creates a new event deduplicator
func NewDeduplicator(window, cleanupInt time.Duration) *Deduplicator {
	d := &Deduplicator{
		cache:      make(map[string]time.Time),
		window:     window,
		cleanupInt: cleanupInt,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	d.wg.Add(1)
	go d.cleanupLoop()

	return d
}

// Check checks if an event is duplicate
// Returns true if duplicate, false if new
func (d *Deduplicator) Check(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// Check if event exists and is within window
	if ts, ok := d.cache[key]; ok {
		if now.Sub(ts) < d.window {
			return true // Duplicate
		}
		// Expired, will be replaced
	}

	// Record new event
	d.cache[key] = now
	return false // New event
}

// cleanupLoop periodically removes expired entries
func (d *Deduplicator) cleanupLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.cleanupInt)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.cleanup()
		case <-d.done:
			return
		}
	}
}

// cleanup removes expired entries
func (d *Deduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, ts := range d.cache {
		if now.Sub(ts) > d.window {
			delete(d.cache, key)
		}
	}
}

// Shutdown stops the deduplicator and waits for goroutines to exit
func (d *Deduplicator) Shutdown() {
	close(d.done)
	d.wg.Wait()
}

// Size returns the current cache size (for testing)
func (d *Deduplicator) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.cache)
}
