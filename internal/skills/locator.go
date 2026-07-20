package skills

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultTTL      = 5 * time.Minute
	maxCacheEntries = 100
	sweepInterval   = 5 * time.Minute
)

type cacheEntry struct {
	skills    []Skill
	expiresAt time.Time
}

// Locator discovers skills from the filesystem with TTL-based caching.
type Locator struct {
	log   *slog.Logger
	cache map[string]*cacheEntry
	mu    sync.RWMutex // protects cache
	// installMu serializes filesystem-mutating skill CRUD (Install/Delete).
	// Install is check-then-rename (hasManagedSkill → RemoveAll → Rename); without
	// serialization, concurrent replace=true installs silently lose data (T2's
	// RemoveAll deletes T1's just-renamed dir) and concurrent new-name installs race
	// to ENOTEMPTY → spurious 500 instead of the contracted 409. Distinct from cache
	// mu (read-heavy, would over-contend if merged). Skill writes are infrequent
	// (admin/owner ops), so a single global lock is sufficient.
	installMu sync.Mutex
	ttl       time.Duration
	stopCh    chan struct{}
	closeOnce sync.Once
}

// NewLocator creates a skill locator with TTL cache.
func NewLocator(log *slog.Logger, ttl time.Duration) *Locator {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	l := &Locator{
		log:    log,
		cache:  make(map[string]*cacheEntry),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go l.sweep()
	return l
}

// List returns deduplicated skills for the given homeDir and workDir.
// Returns cached results if fresh; otherwise scans filesystem.
func (l *Locator) List(_ context.Context, homeDir, workDir string) ([]Skill, error) {
	key := workDir

	l.mu.RLock()
	if e, ok := l.cache[key]; ok && time.Now().Before(e.expiresAt) {
		skills := e.skills
		l.mu.RUnlock()
		return skills, nil
	}
	l.mu.RUnlock()

	skills := scanDirs(homeDir, workDir)

	l.mu.Lock()
	// Evict oldest if at capacity
	if len(l.cache) >= maxCacheEntries {
		l.evictOldest()
	}
	l.cache[key] = &cacheEntry{
		skills:    skills,
		expiresAt: time.Now().Add(l.ttl),
	}
	l.mu.Unlock()

	return skills, nil
}

// wsInstalledCacheKey 与 List 的缓存键（workDir）隔离：List 返回 global+project
// 合并结果，ListWorkspaceInstalled 仅返回 <workDir>/.agents/skills 受管 skill，二者
// 同以 workDir 为维度但内容不同，故用前缀区分。workDir 为文件系统路径（以 "/" 或盘符
// 开头），不会与该前缀碰撞。Invalidate(workDir) 需同时清除两个键（见下）。
func wsInstalledCacheKey(workDir string) string { return "ws-installed:" + workDir }

// ListWorkspaceInstalled 仅返回指定 workspace 下「安装的」受管 skill
// （<workDir>/.agents/skills，source=project & managed=true），不含全局、不含
// <workDir>/.claude/skills 只读目录、不含其他 workspace。用于
// GET /api/workspaces/{wid}/skills（issue #918）：workspace 管理面只列本 workspace
// 安装的 skill。走独立 TTL 缓存键；workspace 写操作经 Invalidate 同步失效该键。
func (l *Locator) ListWorkspaceInstalled(_ context.Context, workDir string) ([]Skill, error) {
	key := wsInstalledCacheKey(workDir)

	l.mu.RLock()
	if e, ok := l.cache[key]; ok && time.Now().Before(e.expiresAt) {
		skills := e.skills
		l.mu.RUnlock()
		return skills, nil
	}
	l.mu.RUnlock()

	skills := scanWorkspaceInstalled(workDir)

	l.mu.Lock()
	// Evict oldest if at capacity
	if len(l.cache) >= maxCacheEntries {
		l.evictOldest()
	}
	l.cache[key] = &cacheEntry{
		skills:    skills,
		expiresAt: time.Now().Add(l.ttl),
	}
	l.mu.Unlock()

	return skills, nil
}

// ListMerged 合并全局（homeDir 范围）与多个 workspace 的 skill 列表，去重
// （workspace project 覆盖 global 同名）。workDirs 为空或全空时仅返回 global。
//
// 用于 GET /api/skills：一个用户可能拥有多个 workspace，此方法把它们的 managed
// skill 合并进全局视图。各 workDir 仍走各自的 TTL 缓存；写入后由调用方 Invalidate。
//
// ctx 向下传播至 List（符合请求路径禁用 context.Background 的约定）。注意 List→scanDirs
// 当前为同步 os.ReadDir 遍历、不响应取消；多 workspace 大目录扫描的 ctx-aware 改造
// （filepath.WalkDir + ctx 检查）作为后续优化，此处先打通 ctx 链路。
func (l *Locator) ListMerged(ctx context.Context, homeDir string, workDirs []string) ([]Skill, error) {
	seen := make(map[string]int) // name → result index
	var result []Skill
	add := func(s Skill) {
		if idx, ok := seen[s.Name]; ok {
			// project 覆盖 global（与 dedup 一致）；同名 project 取先到者。
			if s.Source == SourceProject && result[idx].Source == SourceGlobal {
				result[idx] = s
			}
		} else {
			seen[s.Name] = len(result)
			result = append(result, s)
		}
	}

	if homeDir != "" {
		if g, err := l.List(ctx, homeDir, ""); err == nil {
			for _, s := range g {
				add(s)
			}
		}
	}
	for _, wd := range workDirs {
		if wd == "" {
			continue
		}
		ss, err := l.List(ctx, homeDir, wd)
		if err != nil {
			continue
		}
		for _, s := range ss {
			if s.Source == SourceProject { // global 已由上面加入
				add(s)
			}
		}
	}
	return result, nil
}

// Invalidate drops the cached skills for a single workDir. Call after a
// workspace-scoped CRUD so the next List re-scans the filesystem instead of
// serving a stale merged list. Safe to call for a workDir that was never cached.
// 同时清除 ListWorkspaceInstalled 的独立缓存键，避免 workspace 写后该端点回陈旧列表。
func (l *Locator) Invalidate(workDir string) {
	l.mu.Lock()
	delete(l.cache, workDir)
	delete(l.cache, wsInstalledCacheKey(workDir))
	l.mu.Unlock()
}

// InvalidateAll drops every cached entry. Call after a global-scoped CRUD — a
// global skill change invalidates the merged list (global + workspace) of every
// workspace, so a single Invalidate(workDir) is not enough.
func (l *Locator) InvalidateAll() {
	l.mu.Lock()
	l.cache = make(map[string]*cacheEntry)
	l.mu.Unlock()
}

// Close stops the background sweep goroutine.
// It is safe to call multiple times (uses sync.Once).
func (l *Locator) Close() {
	l.closeOnce.Do(func() {
		close(l.stopCh)
	})
}

func (l *Locator) sweep() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for k, e := range l.cache {
				if now.After(e.expiresAt) {
					delete(l.cache, k)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			l.log.Debug("skills: sweep goroutine stopped")
			return
		}
	}
}

func (l *Locator) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range l.cache {
		if oldestKey == "" || e.expiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.expiresAt
		}
	}
	if oldestKey != "" {
		delete(l.cache, oldestKey)
	}
}
