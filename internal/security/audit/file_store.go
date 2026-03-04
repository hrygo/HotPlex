package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/security"
)

// Compile-time interface verification
var _ security.AuditStore = (*FileAuditStore)(nil)

// FileAuditStore provides file-based audit storage in JSON Lines format.
type FileAuditStore struct {
	mu       sync.Mutex
	filename string
	file     *os.File
	writer   *json.Encoder
}

// NewFileAuditStore creates a new FileAuditStore.
func NewFileAuditStore(filename string) (*FileAuditStore, error) {
	if filename == "" {
		return nil, errors.New("audit: filename cannot be empty")
	}

	// Ensure directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("audit: failed to create directory: %w", err)
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("audit: failed to open file: %w", err)
	}

	return &FileAuditStore{
		filename: filename,
		file:     file,
		writer:   json.NewEncoder(file),
	}, nil
}

// Save saves an audit event to the file.
func (f *FileAuditStore) Save(ctx context.Context, event *security.AuditEvent) error {
	if event == nil {
		return errors.New("audit: event cannot be nil")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Set timestamp if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Set ID if not set
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	return f.writer.Encode(event)
}

// Query retrieves audit events matching the filter.
// Note: This loads all events from the file and filters in memory.
// For large datasets, consider using MemoryAuditStore.
func (f *FileAuditStore) Query(ctx context.Context, filter security.AuditFilter) ([]security.AuditEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Re-open file for reading
	file, err := os.Open(f.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []security.AuditEvent{}, nil
		}
		return nil, fmt.Errorf("audit: failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	results := make([]security.AuditEvent, 0)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var event security.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Skip malformed lines
		}

		if f.matchesFilter(event, filter) {
			results = append(results, event)
		}

		// Apply limit
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}

	return results, scanner.Err()
}

// matchesFilter checks if an event matches the filter.
func (f *FileAuditStore) matchesFilter(event security.AuditEvent, filter security.AuditFilter) bool {
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
// Note: This scans the entire file.
func (f *FileAuditStore) Stats(ctx context.Context) (security.AuditStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	file, err := os.Open(f.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return security.AuditStats{
				ByLevel:     make(map[string]int64),
				ByCategory:  make(map[string]int64),
				BySource:    make(map[string]int64),
				TopPatterns: make([]security.PatternStat, 0),
				TimeSeries:  make([]security.TimeBucket, 0),
			}, nil
		}
		return security.AuditStats{}, fmt.Errorf("audit: failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

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
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var event security.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		switch event.Action {
		case security.AuditActionBlocked:
			stats.TotalBlocked++
		case security.AuditActionApproved, security.AuditActionBypassed:
			stats.TotalApproved++
		}

		stats.ByLevel[event.Level.String()]++
		stats.ByCategory[event.Category]++
		stats.BySource[event.Source]++

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
		if !found {
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
	if len(stats.TopPatterns) > 10 {
		stats.TopPatterns = stats.TopPatterns[:10]
	}

	return stats, scanner.Err()
}

// Close closes the file.
func (f *FileAuditStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file != nil {
		return f.file.Close()
	}
	return nil
}
