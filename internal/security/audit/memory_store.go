package audit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/security"
)

// Compile-time interface verification
var _ security.AuditStore = (*MemoryAuditStore)(nil)

// MemoryAuditStore provides in-memory audit storage with a circular buffer.
type MemoryAuditStore struct {
	mu       sync.RWMutex
	events   []security.AuditEvent
	capacity int
	index    int
	count    int
}

// NewMemoryAuditStore creates a new MemoryAuditStore with the given capacity.
func NewMemoryAuditStore(capacity int) *MemoryAuditStore {
	if capacity <= 0 {
		capacity = 1000
	}
	return &MemoryAuditStore{
		events:   make([]security.AuditEvent, capacity),
		capacity: capacity,
	}
}

// Save saves an audit event to the store.
func (m *MemoryAuditStore) Save(ctx context.Context, event *security.AuditEvent) error {
	if event == nil {
		return errors.New("audit: event cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep copy to prevent external mutation
	copied := *event
	if event.Metadata != nil {
		copied.Metadata = make(map[string]any, len(event.Metadata))
		for k, v := range event.Metadata {
			copied.Metadata[k] = v
		}
	}

	m.events[m.index] = copied
	m.index = (m.index + 1) % m.capacity
	if m.count < m.capacity {
		m.count++
	}
	return nil
}

// Query retrieves audit events matching the filter.
func (m *MemoryAuditStore) Query(ctx context.Context, filter security.AuditFilter) ([]security.AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := filter.Limit
	if limit <= 0 || limit > m.count {
		limit = m.count
	}

	results := make([]security.AuditEvent, 0, limit)

	// Iterate through events in reverse order (newest first)
	for i := 0; i < m.count && len(results) < limit; i++ {
		idx := (m.index - 1 - i + m.capacity) % m.capacity
		event := m.events[idx]

		if !m.matchesFilter(event, filter) {
			continue
		}
		results = append(results, event)
	}

	return results, nil
}

// matchesFilter checks if an event matches the filter.
func (m *MemoryAuditStore) matchesFilter(event security.AuditEvent, filter security.AuditFilter) bool {
	if !filter.StartTime.IsZero() && event.Timestamp.Before(filter.StartTime) {
		return false
	}
	if !filter.EndTime.IsZero() && event.Timestamp.After(filter.EndTime) {
		return false
	}
	if len(filter.Levels) > 0 {
		found := false
		for _, l := range filter.Levels {
			if event.Level == l {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Categories) > 0 {
		found := false
		for _, c := range filter.Categories {
			if event.Category == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Actions) > 0 {
		found := false
		for _, a := range filter.Actions {
			if event.Action == a {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filter.UserID != "" && event.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && event.SessionID != filter.SessionID {
		return false
	}
	return true
}

// Stats returns aggregated statistics.
func (m *MemoryAuditStore) Stats(ctx context.Context) (security.AuditStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := security.AuditStats{
		TotalBlocked:  0,
		TotalApproved: 0,
		ByLevel:       make(map[string]int64),
		ByCategory:    make(map[string]int64),
		BySource:      make(map[string]int64),
		TopPatterns:   make([]security.PatternStat, 0),
		TimeSeries:    make([]security.TimeBucket, 0),
	}

	patternCounts := make(map[string]int64)
	now := time.Now()

	// Process events in reverse order
	for i := 0; i < m.count; i++ {
		idx := (m.index - 1 - i + m.capacity) % m.capacity
		event := m.events[idx]

		switch event.Action {
		case security.AuditActionBlocked:
			stats.TotalBlocked++
		case security.AuditActionApproved, security.AuditActionBypassed:
			stats.TotalApproved++
		}

		stats.ByLevel[event.Level.String()]++
		stats.ByCategory[event.Category]++
		stats.BySource[event.Source]++

		// Count patterns
		if event.Operation != "" {
			patternCounts[event.Operation]++
		}

		// Time bucket (hourly)
		bucketTime := event.Timestamp.Truncate(time.Hour)
		found := false
		for i := range stats.TimeSeries {
			if stats.TimeSeries[i].Timestamp.Equal(bucketTime) {
				stats.TimeSeries[i].Count++
				found = true
				break
			}
		}
		if !found && len(stats.TimeSeries) < 24 {
			stats.TimeSeries = append(stats.TimeSeries, security.TimeBucket{
				Timestamp: bucketTime,
				Count:     1,
			})
		}
	}

	// Sort time series
	sort.Slice(stats.TimeSeries, func(i, j int) bool {
		return stats.TimeSeries[i].Timestamp.Before(stats.TimeSeries[j].Timestamp)
	})

	// Top patterns
	for pattern, count := range patternCounts {
		stats.TopPatterns = append(stats.TopPatterns, security.PatternStat{
			Pattern: pattern,
			Count:   count,
		})
	}
	// Sort by count descending
	sort.Slice(stats.TopPatterns, func(i, j int) bool {
		return stats.TopPatterns[i].Count > stats.TopPatterns[j].Count
	})
	// Keep top 10
	if len(stats.TopPatterns) > 10 {
		stats.TopPatterns = stats.TopPatterns[:10]
	}

	// Remove empty time buckets older than 24 hours
	cutoff := now.Add(-24 * time.Hour)
	validBuckets := make([]security.TimeBucket, 0)
	for _, tb := range stats.TimeSeries {
		if tb.Timestamp.After(cutoff) {
			validBuckets = append(validBuckets, tb)
		}
	}
	stats.TimeSeries = validBuckets

	return stats, nil
}

// Close closes the store (no-op for memory store).
func (m *MemoryAuditStore) Close() error {
	return nil
}
