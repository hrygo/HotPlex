package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
)

// nativeCatalogQueryTimeout bounds every authoritative Worker catalog query.
// A Worker that cannot answer within the bound degrades to a
// filesystem-discoverable-only catalog — "cannot confirm" must never be read
// as "callable" (spec §8.2, §8.5).
const nativeCatalogQueryTimeout = 5 * time.Second

// sessionCatalogStore is the session-scoped merged command catalog. It caches
// one assembled catalog per session, keyed by (worker instance, generation),
// and is invalidated whenever the session's Worker binding or context changes
// (/reset, /cd, worker attach/replacement) — spec §5.2, §8.7.
type sessionCatalogStore struct {
	// mu guards entries. Never embedded, never passed by value.
	mu            sync.Mutex
	entries       map[string]catalogEntry
	log           *slog.Logger
	skillsLocator SkillsLocator
	// queryTimeout bounds the authoritative Worker catalog query. The
	// production default is nativeCatalogQueryTimeout; tests shorten it.
	queryTimeout time.Duration
}

// catalogEntry is one session's assembled catalog snapshot.
type catalogEntry struct {
	gen         uint64
	worker      worker.Worker
	descriptors []worker.NativeCommandDescriptor
	fetchedAt   time.Time
}

// newSessionCatalogStore creates an empty catalog store. The locator is
// optional: a nil locator simply omits the filesystem tier from assembly.
func newSessionCatalogStore(log *slog.Logger, locator SkillsLocator) *sessionCatalogStore {
	if log == nil {
		log = slog.Default()
	}
	return &sessionCatalogStore{
		entries:       make(map[string]catalogEntry),
		log:           log,
		skillsLocator: locator,
		queryTimeout:  nativeCatalogQueryTimeout,
	}
}

// Lookup returns the merged session-scoped command catalog for the current
// Worker. The result is assembled fresh on a cache miss and cached on hit;
// a hit requires BOTH the exact worker instance and the exact session
// generation (spec §8.7), so /reset, /cd, and worker replacement all force a
// refetch.
//
// Merge precedence (spec §5.2): Gateway fixed commands (highest, gated by
// per-command Worker capability conditions) > the Worker's authoritative
// catalog > HotPlex filesystem skills (always Kind=skill, StartsTurn=true,
// discoverable-only). The merge key is the case-sensitive canonical name;
// duplicate names resolve by precedence.
//
// The error return signals authoritative-catalog degradation: when the
// Worker's NativeCommandCatalogProvider query fails (or times out), the
// returned descriptors still include the fixed commands and filesystem
// entries, but no entry may be marked callable — the caller must treat the
// result as discoverable-only and the error is non-nil so that degradation is
// explicit (spec §8.5). A nil error means the authoritative tier was present.
func (s *sessionCatalogStore) Lookup(ctx context.Context, sessionID, workDir string, w worker.Worker, gen uint64) ([]worker.NativeCommandDescriptor, error) {
	if cached, ok := s.cached(sessionID, w, gen); ok {
		return cached, nil
	}
	descriptors, err := s.assemble(ctx, sessionID, workDir, w)
	if err == nil {
		s.store(sessionID, w, gen, descriptors)
	}
	return descriptors, err
}

// Invalidate drops the session's cached catalog entry so the next Lookup
// assembles fresh. Called alongside a per-session generation bump on /reset,
// /cd, and every Worker attach (spec §5.2).
func (s *sessionCatalogStore) Invalidate(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sessionID)
}

// cached returns the session's catalog only when both the worker instance and
// the generation match the current lookup (spec §8.7).
func (s *sessionCatalogStore) cached(sessionID string, w worker.Worker, gen uint64) ([]worker.NativeCommandDescriptor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[sessionID]
	if !ok || entry.worker != w || entry.gen != gen {
		return nil, false
	}
	return entry.descriptors, true
}

func (s *sessionCatalogStore) store(sessionID string, w worker.Worker, gen uint64, descriptors []worker.NativeCommandDescriptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[sessionID] = catalogEntry{
		gen:         gen,
		worker:      w,
		descriptors: descriptors,
		fetchedAt:   time.Now(),
	}
}

// assemble builds the merged catalog for the given session Worker. The
// returned error is non-nil only when the Worker's authoritative catalog
// could not be fetched, in which case the merged result degrades to fixed
// commands + filesystem entries (discoverable-only, spec §8.5).
func (s *sessionCatalogStore) assemble(ctx context.Context, sessionID, workDir string, w worker.Worker) ([]worker.NativeCommandDescriptor, error) {
	if w == nil {
		return nil, fmt.Errorf("gateway: native catalog lookup requires an attached worker")
	}

	merged := make([]worker.NativeCommandDescriptor, 0, len(nativeFixedCommands)+8)
	seen := make(map[string]struct{}, len(nativeFixedCommands)+8)

	// Tier 1 — Gateway fixed commands. Highest precedence; each entry carries
	// a Worker capability predicate so commands the current Worker cannot
	// honor never surface as available (G4 fix, spec §5.2).
	for _, fc := range nativeFixedCommands {
		if fc.requires != nil && !fc.requires(w) {
			continue
		}
		// A required fixed name remains reserved even when this Worker type
		// cannot execute the command. This prevents a same-named Worker or
		// filesystem entry from reappearing at a lower merge tier.
		seen[fc.desc.Name] = struct{}{}
		if !nativeFixedCommandVisible(w, fc.desc.Name) {
			continue
		}
		descriptor := fc.desc
		descriptor.CatalogOrigin = worker.CatalogOriginGateway
		merged = append(merged, descriptor)
	}

	// Tier 2 — the Worker's authoritative catalog. Bounded query (spec §8.2);
	// a failure degrades the whole result to discoverable-only and is
	// surfaced as an error so callers never mark entries callable (spec §8.5).
	var authoritativeErr error
	if provider, ok := worker.AsNativeCatalogProvider(w); ok {
		catalogCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
		descriptors, err := provider.ListNativeCommands(catalogCtx, workDir)
		cancel()
		if err != nil {
			authoritativeErr = fmt.Errorf("worker %s native catalog unavailable: %w", w.Type(), err)
			s.log.Warn("gateway: worker native catalog unavailable, commands discoverable-only",
				"session_id", sessionID, "worker_type", string(w.Type()), "error", err)
		} else {
			for _, d := range descriptors {
				if _, dup := seen[d.Name]; dup {
					continue
				}
				// Provider metadata is not evidence of its own trust tier. The
				// assembly boundary stamps every successful authoritative entry
				// as Worker-owned, preventing a provider or stale Path shape from
				// masquerading as a filesystem-only descriptor.
				d.CatalogOrigin = worker.CatalogOriginWorker
				merged = append(merged, d)
				seen[d.Name] = struct{}{}
			}
		}
	}

	// Tier 3 — HotPlex filesystem skills. Included only when the name is not
	// already claimed by a higher tier; always skill-kind, StartsTurn, mode
	// derived from the Worker type (spec §5.2). A filesystem scan failure is
	// non-fatal: the authoritative tiers still stand, just without the
	// discoverable additions.
	if s.skillsLocator != nil {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			s.log.Warn("gateway: failed to resolve home directory, global skills may be incomplete",
				"session_id", sessionID, "worker_type", string(w.Type()), "error", homeErr)
		}
		allSkills, err := s.skillsLocator.List(ctx, homeDir, workDir)
		if err != nil {
			s.log.Warn("gateway: filesystem skill scan failed, skills omitted from catalog",
				"session_id", sessionID, "worker_type", string(w.Type()), "error", err)
		} else {
			mode := worker.NativeModeForType(w.Type())
			for _, sk := range allSkills {
				if _, dup := seen[sk.Name]; dup {
					continue
				}
				merged = append(merged, worker.NativeCommandDescriptor{
					Name:          sk.Name,
					Description:   sk.Description,
					Kind:          worker.NativeCommandKindSkill,
					Mode:          mode,
					StartsTurn:    true,
					AcceptsArgs:   true,
					Path:          sk.FilePath,
					CatalogOrigin: worker.CatalogOriginFilesystem,
				})
				seen[sk.Name] = struct{}{}
			}
		}
	}
	return merged, authoritativeErr
}

// fixedNativeCommand is one Gateway-side fixed command with its Worker
// capability condition. A nil requires predicate means the command is always
// present.
type fixedNativeCommand struct {
	desc     worker.NativeCommandDescriptor
	requires func(w worker.Worker) bool
}

// fixedNativeCommandDesc builds a control-kind, turn-free descriptor for the
// Gateway fixed command table. Control commands are gateway-handled (not
// dispatched through the Worker's native invoker), so Mode and Path stay
// empty.
func fixedNativeCommandDesc(name, description string, acceptsArgs bool) worker.NativeCommandDescriptor {
	return worker.NativeCommandDescriptor{
		Name:        name,
		Description: description,
		Kind:        worker.NativeCommandKindControl,
		StartsTurn:  false,
		AcceptsArgs: acceptsArgs,
	}
}

func requiresControlRequester(w worker.Worker) bool {
	_, ok := w.(worker.ControlRequester)
	return ok
}

func requiresWorkerCommander(w worker.Worker) bool {
	_, ok := w.(worker.WorkerCommander)
	return ok
}

// nativeFixedCommandVisible captures adapter-specific gaps that cannot be
// represented by the shared capability interfaces alone. The requires
// predicate still owns whether a fixed name is reserved; this predicate only
// decides whether that reserved name is advertised as callable.
func nativeFixedCommandVisible(w worker.Worker, name string) bool {
	switch w.Type() {
	case worker.TypeCodexCLI:
		return name != "model" && name != "perm"
	case worker.TypeACP:
		switch name {
		case "compact", "rewind", "mcp":
			return false
		}
	case worker.TypeClaudeCode:
		return name != "clear"
	}
	return true
}

// requiresNativeEffort is the reserved capability predicate for /effort and
// /commit. No current Worker advertises native effort/commit support — the
// WorkerCommander path explicitly rejects both and plain workers only forward
// them as ordinary text — so it returns false and keeps both commands out of
// the merged catalog by default (spec §10.8). The seam exists so a future
// Worker capability interface can opt them in.
func requiresNativeEffort(worker.Worker) bool { return false }

// nativeFixedCommands is the Gateway-side fixed command table (G4 fix). Merge
// precedence guarantees these names can never be shadowed by a Worker skill
// or a filesystem skill of the same case-sensitive name (spec §5.2, §5.3).
var nativeFixedCommands = []fixedNativeCommand{
	// Always present session-control commands (display-only, gateway-handled).
	{desc: fixedNativeCommandDesc("reset", "重置上下文（全新开始）", false)},
	{desc: fixedNativeCommandDesc("stop", "停止当前轮次", false)},
	{desc: fixedNativeCommandDesc("gc", "休眠会话（停止 Worker，保留会话）", false)},
	{desc: fixedNativeCommandDesc("park", "休眠会话（停止 Worker，保留会话）", false)},
	{desc: fixedNativeCommandDesc("new", "重置上下文（全新开始）", false)},
	{desc: fixedNativeCommandDesc("cd", "切换工作目录（创建新会话）", true)},
	{desc: fixedNativeCommandDesc("skills", "查看已加载的技能列表", false)},
	{desc: fixedNativeCommandDesc("help", "显示命令帮助", false)},

	// Control-request commands — require the structured ControlRequester surface.
	{desc: fixedNativeCommandDesc("context", "查看上下文窗口使用量", false), requires: requiresControlRequester},
	{desc: fixedNativeCommandDesc("mcp", "查看 MCP 服务器状态", false), requires: requiresControlRequester},
	{desc: fixedNativeCommandDesc("model", "切换 AI 模型", true), requires: requiresControlRequester},
	{desc: fixedNativeCommandDesc("perm", "设置权限模式", true), requires: requiresControlRequester},

	// Worker-command passthrough commands — require the WorkerCommander surface.
	{desc: fixedNativeCommandDesc("compact", "压缩对话历史", false), requires: requiresWorkerCommander},
	{desc: fixedNativeCommandDesc("clear", "清空对话", false), requires: requiresWorkerCommander},
	{desc: fixedNativeCommandDesc("rewind", "撤销上一轮对话", false), requires: requiresWorkerCommander},

	// Effort/commit — capability-conditioned, excluded by default (spec §10.8).
	{desc: fixedNativeCommandDesc("effort", "设置推理投入", true), requires: requiresNativeEffort},
	{desc: fixedNativeCommandDesc("commit", "创建提交", false), requires: requiresNativeEffort},
}
