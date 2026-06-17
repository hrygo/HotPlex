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

// 指标快照(atomic,gauge 回调无锁读取;snapshotMetricsLocked 持 mu 写)。
var (
	metricActiveSessions     atomic.Int64
	metricDistinctUsers      atomic.Int64
	metricDistinctWorkspaces atomic.Int64
	metricMemoryReserved     atomic.Int64
)

func init() {
	observability.RegisterGaugeCallbacks(func(m metric.Meter) {
		poolGauge, err := m.Float64ObservableGauge(
			"hotplex.pool.utilization",
			metric.WithDescription("Pool utilization ratio (0-1)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.utilization gauge", "err", err)
			return
		}
		activeGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.active_sessions",
			metric.WithDescription("Active worker sessions (global, includes platform/cron)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.active_sessions gauge", "err", err)
			return
		}
		usersGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.distinct_users",
			metric.WithDescription("Distinct users with at least one active session"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.distinct_users gauge", "err", err)
			return
		}
		wsGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.distinct_workspaces",
			metric.WithDescription("Distinct WebChat workspaces with at least one active session (platform sessions excluded)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.distinct_workspaces gauge", "err", err)
			return
		}
		memGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.memory_reserved_bytes",
			metric.WithDescription("Estimated reserved memory in bytes (active sessions x 512MB, global aggregate)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.memory_reserved_bytes gauge", "err", err)
			return
		}
		_, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			o.ObserveFloat64(poolGauge, math.Float64frombits(poolUtilization.Load()))
			o.ObserveInt64(activeGauge, metricActiveSessions.Load())
			o.ObserveInt64(usersGauge, metricDistinctUsers.Load())
			o.ObserveInt64(wsGauge, metricDistinctWorkspaces.Load())
			o.ObserveInt64(memGauge, metricMemoryReserved.Load())
			return nil
		}, poolGauge, activeGauge, usersGauge, wsGauge, memGauge)
	})
}

func setPoolUtilization(v float64) {
	poolUtilization.Store(math.Float64bits(v))
}

// snapshotMetricsLocked 将当前 pool 状态写入 atomic 快照变量供 gauge 回调无锁读取。
// 调用方必须持有 p.mu。
func (p *PoolManager) snapshotMetricsLocked() {
	metricActiveSessions.Store(int64(p.totalCount))
	metricDistinctUsers.Store(int64(len(p.userCount)))
	metricDistinctWorkspaces.Store(int64(len(p.workspaceCount)))
	metricMemoryReserved.Store(p.totalMemoryReserved())
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	} else {
		setPoolUtilization(0)
	}
}

// totalMemoryReserved 返回全局已预留内存(所有用户之和)。调用方持 p.mu。
func (p *PoolManager) totalMemoryReserved() int64 {
	var sum int64
	for _, m := range p.userMemory {
		sum += m
	}
	return sum
}

// PoolManager manages per-user and global concurrency quotas for worker sessions.
type PoolManager struct {
	log *slog.Logger

	mu             sync.Mutex
	totalCount     int
	userCount      map[string]int   // userID → active session count
	userMemory     map[string]int64 // userID → total estimated memory bytes (sum of RLIMIT_AS caps)
	workspaceCount map[string]int   // workspaceID → active session count (WebChat multi-tenant, spec ①)

	maxSize          int   // 0 = unlimited
	maxIdlePerUser   int   // 0 = unlimited
	maxMemoryPerUser int64 // bytes; 0 = unlimited
	maxPerWorkspace  int   // 0 = unlimited (WebChat per-workspace concurrency, spec ①)
}

// Limits 是 PoolManager 的运行时限额集合,镜像 config.PoolConfig。
// 所有字段 0 = unlimited。UpdateLimits 在持 p.mu 下原子覆盖全部字段。
type Limits struct {
	MaxSize          int   // 全局最大活跃 Worker
	MaxIdlePerUser   int   // per-user 最大并发 session
	MaxPerWorkspace  int   // WebChat per-workspace 并发(spec ①)
	MaxMemoryPerUser int64 // bytes;per-user 内存
}

// Default per-worker memory estimate (matches RLIMIT_AS in proc/manager.go).
const workerMemoryEstimate = 512 * 1024 * 1024 // 512 MB

const (
	poolErrKindExhausted              = "exhausted"
	poolErrKindUserQuotaExceeded      = "user_quota_exceeded"
	poolErrKindMemoryExceeded         = "memory_exceeded"
	poolErrKindWorkspaceQuotaExceeded = "workspace_quota_exceeded" // WebChat per-workspace (spec ①)
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
		workspaceCount:   make(map[string]int),
		maxSize:          maxSize,
		maxIdlePerUser:   maxIdlePerUser,
		maxMemoryPerUser: maxMemoryPerUser,
	}
}

// NewPoolManagerWithWorkspace creates a PoolManager with an additional per-workspace
// concurrency limit (WebChat multi-tenant, spec ①). maxPerWorkspace == 0 disables the
// workspace layer (platform/cron sessions are unaffected, equivalent to NewPoolManager).
func NewPoolManagerWithWorkspace(log *slog.Logger, maxSize, maxIdlePerUser int, maxMemoryPerUser int64, maxPerWorkspace int) *PoolManager {
	p := NewPoolManager(log, maxSize, maxIdlePerUser, maxMemoryPerUser)
	p.maxPerWorkspace = maxPerWorkspace
	return p
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
func (p *PoolManager) Acquire(ctx context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(ctx, userID, "", false)
}

// acquireLocked reserves a concurrency slot (and optionally memory) for userID.
// workspaceID != "" (WebChat sessions) additionally enforces the per-workspace limit.
// Caller must hold p.mu.
func (p *PoolManager) acquireLocked(ctx context.Context, userID, workspaceID string, reserveMemory bool) error {
	if p.maxSize > 0 && p.totalCount >= p.maxSize {
		observability.PoolAcquire().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "pool_exhausted")))
		return &PoolError{Kind: poolErrKindExhausted, Current: p.totalCount, Max: p.maxSize}
	}
	if p.maxIdlePerUser > 0 && p.userCount[userID] >= p.maxIdlePerUser {
		observability.PoolAcquire().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "user_quota_exceeded")))
		return &PoolError{Kind: poolErrKindUserQuotaExceeded, UserID: userID, Current: p.userCount[userID], Max: p.maxIdlePerUser}
	}
	if workspaceID != "" && p.maxPerWorkspace > 0 && p.workspaceCount[workspaceID] >= p.maxPerWorkspace {
		observability.PoolAcquire().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "workspace_quota_exceeded")))
		return &PoolError{Kind: poolErrKindWorkspaceQuotaExceeded, Current: p.workspaceCount[workspaceID], Max: p.maxPerWorkspace}
	}
	if reserveMemory && p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used+workerMemoryEstimate > p.maxMemoryPerUser {
			observability.PoolAcquire().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "memory_exceeded")))
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
	if workspaceID != "" {
		p.workspaceCount[workspaceID]++
	}
	p.snapshotMetricsLocked()
	p.log.Debug("pool: acquired", "user_id", userID, "workspace_id", workspaceID, "total", p.totalCount)
	return nil
}

// AcquireWithMemory atomically reserves both concurrency and memory quota for userID.
// Returns nil on success, or a PoolError describing the failure.
// This avoids the TOCTOU race that exists when Acquire + AcquireMemory are called
// separately: between the two calls, another goroutine could observe the slot
// taken but memory not yet reserved, leading to quota accounting drift.
func (p *PoolManager) AcquireWithMemory(ctx context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(ctx, userID, "", true)
}

// AcquireForWorkspace reserves a slot honoring global + per-user + per-workspace +
// memory limits (WebChat multi-tenant, spec ①). workspaceID == "" (platform/cron
// sessions) skips the per-workspace layer — equivalent to AcquireWithMemory.
func (p *PoolManager) AcquireForWorkspace(ctx context.Context, userID, workspaceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(ctx, userID, workspaceID, true)
}

// Release frees a concurrency slot previously acquired for userID.
// Also releases memory quota under the same lock.
func (p *PoolManager) Release(ctx context.Context, userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.releaseCoreLocked(ctx, userID) {
		p.log.Debug("pool: released", "user_id", userID, "total", p.totalCount)
	}
}

// ReleaseForWorkspace releases a slot acquired via AcquireForWorkspace (WebChat
// multi-tenant, spec ①). workspaceID == "" behaves like Release.
func (p *PoolManager) ReleaseForWorkspace(ctx context.Context, userID, workspaceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.releaseCoreLocked(ctx, userID) {
		return
	}
	if workspaceID != "" && p.workspaceCount[workspaceID] > 0 {
		p.workspaceCount[workspaceID]--
		if p.workspaceCount[workspaceID] <= 0 {
			delete(p.workspaceCount, workspaceID)
		}
	}
	p.log.Debug("pool: released", "user_id", userID, "workspace_id", workspaceID, "total", p.totalCount)
}

// releaseCoreLocked decrements user/total/memory counters and returns true on success.
// Returns false (and logs + best-effort memory cleanup) on double-release.
// Caller must hold p.mu.
func (p *PoolManager) releaseCoreLocked(ctx context.Context, userID string) bool {
	if p.userCount[userID] <= 0 || p.totalCount <= 0 {
		p.log.Error("pool: release without acquire — possible double-release", "user_id", userID,
			"user_count", p.userCount[userID], "total", p.totalCount)
		observability.PoolReleaseErrors().Add(ctx, 1)
		// Best-effort memory cleanup to prevent quota leak on accounting bug.
		p.releaseMemoryLocked(userID)
		return false
	}
	p.userCount[userID]--
	if p.userCount[userID] <= 0 {
		delete(p.userCount, userID)
	}
	p.totalCount--
	p.snapshotMetricsLocked()
	p.releaseMemoryLocked(userID)
	return true
}

// Stats returns the current pool utilization.
func (p *PoolManager) Stats() (total, maxSize, uniqueUsers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.totalCount, p.maxSize, len(p.userCount)
}

// UpdateLimits 动态调整全部 4 个限额(spec ⑤)。
// 若某维限额被降到当前占用之下,已运行 session 不被驱逐 —— 新 Acquire 将被拒,
// 直到 session 自然 Release。所有 gauge 快照在此重算(utilization 的分母 maxSize 可能变了)。
func (p *PoolManager) UpdateLimits(l Limits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := Limits{
		MaxSize:          p.maxSize,
		MaxIdlePerUser:   p.maxIdlePerUser,
		MaxPerWorkspace:  p.maxPerWorkspace,
		MaxMemoryPerUser: p.maxMemoryPerUser,
	}
	p.maxSize = l.MaxSize
	p.maxIdlePerUser = l.MaxIdlePerUser
	p.maxPerWorkspace = l.MaxPerWorkspace
	p.maxMemoryPerUser = l.MaxMemoryPerUser
	p.snapshotMetricsLocked()
	p.log.Info("pool: limits updated",
		"old_max", old.MaxSize, "new_max", l.MaxSize,
		"old_per_user", old.MaxIdlePerUser, "new_per_user", l.MaxIdlePerUser,
		"old_per_ws", old.MaxPerWorkspace, "new_per_ws", l.MaxPerWorkspace,
		"old_mem_mb", old.MaxMemoryPerUser/(1024*1024), "new_mem_mb", l.MaxMemoryPerUser/(1024*1024))
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
	p.snapshotMetricsLocked()
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
	p.snapshotMetricsLocked()
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
