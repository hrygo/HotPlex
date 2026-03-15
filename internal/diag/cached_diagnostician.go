package diag

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/brain"
	"github.com/hrygo/hotplex/internal/persistence"
)

// DiagnosticianWithCache wraps Diagnostician with caching and metrics.
type DiagnosticianWithCache struct {
	*Diagnostician
	cache   *DiagnosisCache
	metrics *Metrics
	config  *Config
	logger  *slog.Logger
}

// NewDiagnosticianWithCache creates a new Diagnostician with caching.
func NewDiagnosticianWithCache(
	config *Config,
	historyStore persistence.MessageHistoryStore,
	br brain.Brain,
	logger *slog.Logger,
) *DiagnosticianWithCache {
	if logger == nil {
		logger = slog.Default()
	}

	diag := NewDiagnostician(config, historyStore, br, logger)
	cache := NewDiagnosisCache(30*time.Minute, 100)
	metrics := NewMetrics()

	return &DiagnosticianWithCache{
		Diagnostician: diag,
		cache:         cache,
		metrics:       metrics,
		config:        config,
		logger:        logger.With("component", "diag.cached"),
	}
}

// Diagnose performs diagnosis with caching.
func (d *DiagnosticianWithCache) Diagnose(ctx context.Context, trigger Trigger) (*DiagResult, error) {
	d.metrics.RecordDiagnosisStart(trigger.Type())
	startTime := time.Now()

	// First collect context to check for duplicates
	diagCtx, err := d.collector.Collect(ctx, trigger)
	if err != nil {
		d.metrics.RecordDiagnosisComplete(false, time.Since(startTime))
		return nil, err
	}

	// Check cache for duplicate
	dupResult := d.cache.CheckDuplicate(diagCtx)
	if dupResult.IsDuplicate {
		d.metrics.RecordCacheHit()
		d.logger.Info("Duplicate diagnosis found in cache",
			"hash", dupResult.Hash,
			"session", trigger.SessionID(),
		)
		d.metrics.RecordDiagnosisComplete(true, time.Since(startTime))
		return dupResult.Existing, nil
	}

	d.metrics.RecordCacheMiss()

	// Perform actual diagnosis
	result, err := d.Diagnostician.Diagnose(ctx, trigger)
	if err != nil {
		d.metrics.RecordDiagnosisComplete(false, time.Since(startTime))
		return nil, err
	}

	// Cache the result
	d.cache.Store(diagCtx, result)

	d.metrics.RecordDiagnosisComplete(true, time.Since(startTime))
	d.metrics.RecordPendingAdded()

	return result, nil
}

// ConfirmIssue confirms issue creation with metrics.
func (d *DiagnosticianWithCache) ConfirmIssue(ctx context.Context, diagID string) (string, error) {
	url, err := d.Diagnostician.ConfirmIssue(ctx, diagID)
	if err != nil {
		return "", err
	}

	d.metrics.RecordIssueCreated()
	return url, nil
}

// IgnoreIssue ignores issue with metrics.
func (d *DiagnosticianWithCache) IgnoreIssue(ctx context.Context, diagID string) error {
	err := d.Diagnostician.IgnoreIssue(ctx, diagID)
	if err != nil {
		return err
	}

	d.metrics.RecordIssueIgnored()
	return nil
}

// GetMetrics returns the metrics instance.
func (d *DiagnosticianWithCache) GetMetrics() *Metrics {
	return d.metrics
}

// GetCache returns the cache instance.
func (d *DiagnosticianWithCache) GetCache() *DiagnosisCache {
	return d.cache
}

// GetMetricsSnapshot returns a snapshot of current metrics.
func (d *DiagnosticianWithCache) GetMetricsSnapshot() MetricsSnapshot {
	return d.metrics.Snapshot()
}

// CleanupStale cleans up stale pending diagnoses and cache entries.
func (d *DiagnosticianWithCache) CleanupStale(maxAge time.Duration) int {
	cleanedDiags := d.Diagnostician.CleanupStale(maxAge)
	cleanedCache := d.cache.Cleanup()

	d.logger.Debug("Cleanup complete",
		"diagnoses_cleaned", cleanedDiags,
		"cache_cleaned", cleanedCache,
	)

	return cleanedDiags + cleanedCache
}
