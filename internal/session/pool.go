package session

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/observability"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var poolUtilization atomic.Uint64 // stores math.Float64bits

func init() {
	_, _ = observability.Meter().RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveFloat64(observability.PoolUtilization(), math.Float64frombits(poolUtilization.Load()))
		return nil
	}, observability.PoolUtilization())
}

func setPoolUtilization(v float64) {
	poolUtilization.Store(math.Float64bits(v))
}

// PoolManager manages per-user and global concurrency quotas for worker sessions.
type PoolManager struct {
	log *slog.Logger

	mu         sync.Mutex
	totalCount int
	userCount  map[string]int   // userID → active session count
	userMemory map[string]int64 // userID → total estimated memory bytes (sum of RLIMIT_AS caps)

	maxSize          int   // 0 = unlimited
	maxIdlePerUser   int   // 0 = unlimited
	maxMemoryPerUser int64 // bytes; 0 = unlimited
}

// Default per-worker memory estimate (matches RLIMIT_AS in proc/manager.go).
const workerMemoryEstimate = 512 * 1024 * 1024 // 512 MB

const (
	poolErrKindExhausted         = "exhausted"
	poolErrKindUserQuotaExceeded = "user_quota_exceeded"
	poolErrKindMemoryExceeded    = "memory_exceeded"
)

// NewPoolManager creates a PoolManager with the given limits.
func NewPoolManager(log *slog.Logger, maxSize, maxIdlePerUser int, maxMemoryPerUser int64) *PoolManager {
	if log == nil {
		log = slog.Default()
	}
	return &PoolManager{
		log:              log,
		userCount:        make(map[string]int),
		userMemory:       make(map[string]int64),
		maxSize:          maxSize,
		maxIdlePerUser:   maxIdlePerUser,
		maxMemoryPerUser: maxMemoryPerUser,
	}
}

// PoolError records why a pool operation failed.
type PoolError struct {
	Kind    string
	UserID  string
	Current int
	Max     int
}

// ErrMemoryExceeded is returned when a user's estimated memory usage would exceed MaxMemoryPerUser.
var ErrMemoryExceeded = errors.New("pool: memory exceeded")

func (e *PoolError) Error() string {
	return "pool: " + e.Kind
}

// Acquire attempts to reserve a concurrency slot for userID.
// It returns nil on success, or a PoolError describing the failure.
func (p *PoolManager) Acquire(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(userID, false)
}

// acquireLocked reserves a concurrency slot (and optionally memory) for userID.
// Caller must hold p.mu.
func (p *PoolManager) acquireLocked(userID string, reserveMemory bool) error {
	if p.maxSize > 0 && p.totalCount >= p.maxSize {
		observability.PoolAcquire().Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", "pool_exhausted")))
		return &PoolError{Kind: poolErrKindExhausted, Current: p.totalCount, Max: p.maxSize}
	}
	if p.maxIdlePerUser > 0 && p.userCount[userID] >= p.maxIdlePerUser {
		observability.PoolAcquire().Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", "user_quota_exceeded")))
		return &PoolError{Kind: poolErrKindUserQuotaExceeded, UserID: userID, Current: p.userCount[userID], Max: p.maxIdlePerUser}
	}
	if reserveMemory && p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used+workerMemoryEstimate > p.maxMemoryPerUser {
			observability.PoolAcquire().Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", "memory_exceeded")))
			p.log.Warn("pool: memory quota exceeded", "user_id", userID,
				"used_mb", used/(1024*1024),
				"limit_mb", p.maxMemoryPerUser/(1024*1024),
				"worker_mb", workerMemoryEstimate/(1024*1024))
			return &PoolError{Kind: poolErrKindMemoryExceeded, UserID: userID}
		}
		p.userMemory[userID] = used + workerMemoryEstimate
	}

	p.userCount[userID]++
	p.totalCount++
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
	p.log.Debug("pool: acquired", "user_id", userID, "total", p.totalCount)
	return nil
}

// AcquireWithMemory atomically reserves both concurrency and memory quota for userID.
// Returns nil on success, or a PoolError describing the failure.
// This avoids the TOCTOU race that exists when Acquire + AcquireMemory are called
// separately: between the two calls, another goroutine could observe the slot
// taken but memory not yet reserved, leading to quota accounting drift.
func (p *PoolManager) AcquireWithMemory(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(userID, true)
}

// Release frees a concurrency slot previously acquired for userID.
// Also releases memory quota under the same lock.
func (p *PoolManager) Release(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.userCount[userID] <= 0 || p.totalCount <= 0 {
		p.log.Error("pool: release without acquire — possible double-release", "user_id", userID,
			"user_count", p.userCount[userID], "total", p.totalCount)
		observability.PoolReleaseErrors().Add(context.Background(), 1)
		// Best-effort memory cleanup to prevent quota leak on accounting bug.
		p.releaseMemoryLocked(userID)
		return
	}
	p.userCount[userID]--
	if p.userCount[userID] <= 0 {
		delete(p.userCount, userID)
	}
	p.totalCount--
	total := p.totalCount
	if p.maxSize > 0 {
		setPoolUtilization(float64(total) / float64(p.maxSize))
	}
	p.releaseMemoryLocked(userID)
	p.log.Debug("pool: released", "user_id", userID, "total", total)
}

// Stats returns the current pool utilization.
func (p *PoolManager) Stats() (total, maxSize, uniqueUsers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.totalCount, p.maxSize, len(p.userCount)
}

// UpdateLimits dynamically adjusts the pool limits.
// If maxSize is reduced below the current total, existing sessions are NOT evicted —
// new Acquire calls will be rejected until sessions are naturally released.
func (p *PoolManager) UpdateLimits(maxSize, maxIdlePerUser int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.maxSize
	p.maxSize = maxSize
	p.maxIdlePerUser = maxIdlePerUser
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
	p.log.Info("pool: limits updated", "old_max", old, "new_max", maxSize, "max_per_user", maxIdlePerUser)
}

// Deprecated: AcquireMemory is TOCTOU-unsafe when called separately from
// Acquire. Use AcquireWithMemory instead, which atomically checks both
// slot and memory quota under a single lock.
//
// AcquireMemory reserves memory quota for a user.
// It uses workerMemoryEstimate as the per-worker allocation.
// Returns nil on success, or ErrUserMemoryExceeded if the per-user limit is exceeded.
func (p *PoolManager) AcquireMemory(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used+workerMemoryEstimate > p.maxMemoryPerUser {
			p.log.Warn("pool: memory quota exceeded", "user_id", userID,
				"used_mb", used/(1024*1024),
				"limit_mb", p.maxMemoryPerUser/(1024*1024),
				"worker_mb", workerMemoryEstimate/(1024*1024))
			return &PoolError{Kind: poolErrKindMemoryExceeded, UserID: userID}
		}
		p.userMemory[userID] = used + workerMemoryEstimate
	}
	return nil
}

// Deprecated: AcquireWithMemory + Release handles both slot and memory
// atomically with a single lock.
//
// ReleaseMemory frees memory quota for a user.
func (p *PoolManager) ReleaseMemory(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releaseMemoryLocked(userID)
}

// releaseMemoryLocked frees memory quota. Caller must hold p.mu.
func (p *PoolManager) releaseMemoryLocked(userID string) {
	if p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used >= workerMemoryEstimate {
			p.userMemory[userID] = used - workerMemoryEstimate
		} else if used > 0 {
			p.userMemory[userID] = 0
		}
		if p.userMemory[userID] <= 0 {
			delete(p.userMemory, userID)
		}
	}
}

// UserMemory returns the current estimated memory usage for a user in bytes.
func (p *PoolManager) UserMemory(userID string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userMemory[userID]
}
