package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/noop"
	"github.com/hrygo/hotplex/pkg/events"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// forwardOpts configures the forwardEvents goroutine behavior.
type forwardOpts struct {
	ctx         context.Context
	resumed     bool
	workDir     string
	retryDepth  int
	lastInput   string
	workerRunID string
}

// workerLaunchParams holds the parameters for createAndLaunchWorker.
type workerLaunchParams struct {
	ctx                context.Context
	wt                 worker.WorkerType
	workerInfo         worker.SessionInfo
	platform           string
	botID              string
	botName            string
	forwardOpts        *forwardOpts
	injectExclude      []string          // per-session agent config files to skip; nil = use platform default
	workspaceOverrides map[string]string // WebChat per-workspace config overrides (spec ②); nil = Message Channel track → Load
}

// workerStartFunc is called after AttachWorker and injectAgentConfig.
// On startFn failure, the worker is automatically detached.
type workerStartFunc func(ctx context.Context, w worker.Worker, info worker.SessionInfo) error

// workerAttachErrFunc is called when AttachWorker fails for caller-specific cleanup.
type workerAttachErrFunc func(w worker.Worker, err error)

// createAndLaunchWorker creates a worker, attaches it, injects config, calls startFn,
// and launches the forwardEvents goroutine. Returns the worker for post-launch use.
// On startFn failure, the worker is detached before returning.
func (b *Bridge) createAndLaunchWorker(params workerLaunchParams, startFn workerStartFunc, attachErrFn workerAttachErrFunc) (worker.Worker, error) {
	sid := params.workerInfo.SessionID
	if params.forwardOpts == nil {
		params.forwardOpts = &forwardOpts{}
	}
	if params.forwardOpts.ctx == nil {
		params.forwardOpts.ctx = params.ctx
	}
	if params.forwardOpts.workerRunID == "" {
		params.forwardOpts.workerRunID = "run_" + uuid.NewString()
	}

	start := time.Now()
	defer func() {
		observability.WorkerCreationDuration().Record(params.ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("worker_type", string(params.wt))))
	}()

	w, err := b.wf.NewWorker(params.wt)
	if err != nil {
		return nil, fmt.Errorf("bridge: create worker: %w", err)
	}

	if noopw, ok := w.(*noop.Worker); ok {
		noopw.SetConn(noop.NewConn(sid, params.workerInfo.UserID))
	}

	if err := b.sm.AttachWorker(params.ctx, sid, w); err != nil {
		if attachErrFn != nil {
			attachErrFn(w, err)
		}
		return nil, fmt.Errorf("bridge: attach worker: %w", err)
	}

	b.injectAgentConfig(&params.workerInfo, params.platform, params.botName, params.botID, params.injectExclude, params.workspaceOverrides)

	if err := startFn(params.ctx, w, params.workerInfo); err != nil {
		b.sm.DetachWorker(sid)
		return nil, err
	}

	// Best-effort async persist so WorkerSessionID survives gateway restart
	// even if no turn events arrive (SIGTERM before first Prompt). The
	// correctness guarantee comes from forwardEvents' first-event safety-net.
	// Tracked by fwdWg so graceful shutdown waits for the DB write to complete.
	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("bridge: panic in persistWorkerSessionID", "session_id", sid, "panic", r)
			}
		}()
		b.persistWorkerSessionIDInternal(params.ctx, w, sid, false)
	}()

	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		b.forwardEvents(w, sid, *params.forwardOpts)
	}()

	return w, nil
}

func (b *Bridge) persistWorkerSessionIDEnsure(ctx context.Context, w worker.Worker, sessionID string) {
	b.persistWorkerSessionIDInternal(ctx, w, sessionID, true)
}

func (b *Bridge) persistWorkerSessionIDInternal(_ context.Context, w worker.Worker, sessionID string, force bool) {
	// Use Background to fully detach from the request context. The async DB
	// write outlives the request, so inheriting trace baggage would mislead
	// trace analysis (spans from a completed request's children).
	ctx := context.Background()
	handler, ok := w.(worker.WorkerSessionIDHandler)
	if !ok {
		return
	}
	workerSID := handler.GetWorkerSessionID()
	if workerSID == "" {
		return
	}
	var err error
	if force {
		err = b.sm.EnsureWorkerSessionID(ctx, sessionID, workerSID)
	} else {
		err = b.sm.UpdateWorkerSessionID(ctx, sessionID, workerSID)
	}
	if err != nil {
		b.log.Warn("bridge: failed to persist worker session ID", "session_id", sessionID, "worker_session_id", workerSID, "err", err)
	} else {
		b.log.Debug("bridge: persisted worker session ID", "session_id", sessionID, "worker_session_id", workerSID)
	}
}

// fallbackParams carries the context needed by attemptResumeFallback.
type fallbackParams struct {
	sessionID     string
	workDir       string
	exitCode      int
	retryDepth    int
	workerType    worker.WorkerType
	lastInput     string
	crashedWorker worker.Worker
	sessPlatform  string
	sessOwner     string
	accGeneration int64
	accModelName  string
}

// attemptResumeFallback handles a crashed worker with a two-step strategy:
//  1. retryDepth < 1: Retry resume once to preserve conversation history (transient failures).
//  2. retryDepth >= 1: Fall back to fresh start — conversation data is permanently lost.
//
// Applies to both fresh and resumed sessions. Workers that cannot resume
// will gracefully fall back to fresh Start() via ErrFellBackToFreshStart.
//
// Returns true if a new forwardEvents goroutine took over.
func (b *Bridge) attemptResumeFallback(p fallbackParams) bool {
	b.log.Warn("bridge: worker crashed shortly after resume",
		"session_id", p.sessionID, "worker_type", p.workerType, "exit_code", p.exitCode, "retry_depth", p.retryDepth)

	// Crash-loop protection: if this session has crashed crashLoopMax times within
	// crashLoopWindow, stop retrying to prevent thread exhaustion that can kill
	// the entire gateway process (pthread_create failed → SIGABRT).
	if b.recordCrashLoop(p.sessionID) {
		b.log.Error("bridge: crash loop detected, aborting retry to protect gateway",
			"session_id", p.sessionID, "worker_type", p.workerType, "max", crashLoopMax, "window", crashLoopWindow)
		b.sendError(p.sessionID, events.ErrCodeWorkerCrash, "Worker crashed %d times in %v. Stopping retry to protect gateway stability.", crashLoopMax, crashLoopWindow)
		return false
	}

	// Clean up the crashed worker first.
	b.cleanupCrashedWorker(p.sessionID, p.crashedWorker)

	// Step 1: Retry resume once for transient failures (e.g., file lock, timing).
	if p.retryDepth == 0 {
		if err := b.resumeWithOpts(context.Background(), p.sessionID, p.workDir, forwardOpts{resumed: true, workDir: p.workDir, retryDepth: p.retryDepth + 1, lastInput: p.lastInput}); err != nil {
			if errors.Is(err, worker.ErrResumeCheckFailed) {
				b.log.Warn("bridge: resume verification failed during crash recovery; preserving session context",
					"session_id", p.sessionID, "worker_type", p.workerType, "err", err)
				b.sendError(p.sessionID, events.ErrCodeInternalError, "Unable to verify the previous worker session. Please retry later.")
				return false
			}
			b.log.Warn("bridge: resume retry failed synchronously, falling back to fresh start", "session_id", p.sessionID, "worker_type", p.workerType, "err", err)
		} else {
			b.log.Info("bridge: resume retry succeeded", "session_id", p.sessionID, "worker_type", p.workerType)
			b.sendError(p.sessionID, events.ErrCodeResumeRetry, "Worker crashed after resume (exit %d), retried resume to preserve conversation.", p.exitCode)
			return true
		}
	}

	// Step 2: Resume retry also failed or retryDepth exhausted — start fresh worker.
	b.log.Info("bridge: starting fresh worker after failed resume", "session_id", p.sessionID, "worker_type", p.workerType)

	si, err := b.sm.Get(context.Background(), p.sessionID)
	if err != nil {
		b.log.Error("bridge: session not found for fresh start fallback", "session_id", p.sessionID, "err", err)
		return false
	}

	if si.State == events.StateTerminated {
		if err := b.sm.Transition(context.Background(), p.sessionID, events.StateRunning); err != nil {
			b.log.Error("bridge: pre-attach transition for fresh start", "session_id", p.sessionID, "err", err)
			return false
		}
		si.State = events.StateRunning
	}

	workerInfo := b.prepareWorkerInfo(si.ID, si.UserID, p.workDir, si)

	w, err := b.createAndLaunchWorker(workerLaunchParams{
		ctx:                context.Background(),
		wt:                 si.WorkerType,
		workerInfo:         workerInfo,
		platform:           si.Platform,
		botID:              si.BotID,
		botName:            si.BotName,
		forwardOpts:        &forwardOpts{workDir: p.workDir},
		injectExclude:      nil, // resolved by injectAgentConfig
		workspaceOverrides: b.resolveWorkspaceOverrides(context.Background(), si.WorkspaceID),
	},
		func(ctx context.Context, w worker.Worker, info worker.SessionInfo) error {
			if si.State != events.StateRunning {
				if err := b.sm.Transition(ctx, p.sessionID, events.StateRunning); err != nil {
					b.log.Warn("bridge: transition to running for fresh start", "session_id", p.sessionID, "err", err)
				}
			}
			if err := w.Start(ctx, info); err != nil {
				b.log.Warn("bridge: fresh worker start failed", "session_id", p.sessionID, "err", err)
				return err
			}
			return nil
		},
		func(_ worker.Worker, err error) {
			b.log.Error("bridge: attach worker for fresh start", "session_id", p.sessionID, "err", err)
		},
	)
	if err != nil {
		return false
	}

	// Re-deliver the original input that was lost when the first worker crashed.
	b.captureSyntheticEvent(syntheticTurnParams{
		SessionID:  p.sessionID,
		Reason:     "fresh_start",
		Message:    "Session restarted with context reset after worker crash",
		Source:     eventstore.SourceFreshStart,
		Platform:   p.sessPlatform,
		Owner:      p.sessOwner,
		Model:      p.accModelName,
		Generation: p.accGeneration,
		TurnNum:    0, // fresh start resets turn count
	})
	if p.lastInput != "" {
		b.log.Info("bridge: re-delivering input to fresh worker", "session_id", p.sessionID, "content_len", len(p.lastInput))
		if err := w.Input(context.Background(), p.lastInput, nil); err != nil {
			b.log.Warn("bridge: input re-delivery failed", "session_id", p.sessionID, "err", err)
		}
	}

	b.log.Info("bridge: fresh worker started after resume failure", "session_id", p.sessionID, "worker_type", p.workerType)
	notifyMsg := buildNotifyEnvelope(p.sessionID,
		"🔄 会话已重新启动，上下文已重置。",
		b.hub.NextSeq(p.sessionID))
	_ = b.hub.SendToSession(context.Background(), notifyMsg)
	return true
}

// cleanupCrashedWorker detaches the dead worker and transitions the session to TERMINATED
// so the next message triggers orphan resume instead of silently dropping input.
// Uses CAS via crashedWorker to avoid detaching a worker that was already replaced
// by a concurrent ResumeSession or attemptResumeFallback.
func (b *Bridge) cleanupCrashedWorker(sessionID string, crashedWorker worker.Worker) {
	acc := b.getOrInitAccum(sessionID, "", time.Now())
	b.log.Debug("bridge: cleaning up crashed worker", "session_id", sessionID, "turn_count", acc.TurnCount.Load())
	if b.sm == nil {
		return
	}
	if crashedWorker != nil {
		if !b.sm.DetachWorkerIf(sessionID, crashedWorker) {
			b.log.Debug("bridge: crashed worker already replaced, skipping cleanup", "session_id", sessionID)
			return
		}
	} else {
		b.sm.DetachWorker(sessionID)
	}
	if err := b.sm.Transition(context.Background(), sessionID, events.StateTerminated); err != nil {
		b.log.Debug("bridge: transition to terminated after worker exit", "session_id", sessionID, "err", err)
	}
	b.accumMu.Lock()
	delete(b.accum, sessionID)
	b.accumMu.Unlock()
	b.compressCache.Delete(sessionID)

	b.crashTrackerMu.Lock()
	delete(b.crashTracker, sessionID)
	b.crashTrackerMu.Unlock()
}

// resolveInjectExclude returns the inject_exclude list for a platform, falling
// back from the per-session value to the platform/global default in the atomic
// config map. Used by injectAgentConfig and crash recovery paths.
func (b *Bridge) resolveInjectExclude(platform string, perSession []string) []string {
	if perSession != nil {
		return perSession
	}
	if m, ok := b.agentConfigExclude.Load().(map[string][]string); ok {
		if excl, found := m[platform]; found {
			return slices.Clone(excl)
		}
		if excl, found := m[""]; found {
			return slices.Clone(excl)
		}
	}
	return nil
}

// resolveWorkspaceOverrides fetches a workspace's agent-config overrides and parses
// them. Returns nil for empty workspaceID (Message Channel track) or nil wsStore, and
// degrades to nil (team defaults) on any fetch/parse error — never blocks worker launch.
// ctx propagates request-scoped cancellation/deadline to the workspace DB query (the
// store layer uses standard QueryRowContext without otelsql instrumentation, so ctx
// does not auto-generate an OTel DB span). See design spec §7.3.
func (b *Bridge) resolveWorkspaceOverrides(ctx context.Context, workspaceID string) map[string]string {
	if workspaceID == "" || b.wsStore == nil {
		return nil
	}
	ws, err := b.wsStore.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		b.warnOverrideDegrade(workspaceID, "fetch workspace overrides failed, degrading to team defaults", err)
		return nil
	}
	overrides, err := agentconfig.ValidateOverrides(ws.AgentConfigOverrides)
	if err != nil {
		b.warnOverrideDegrade(workspaceID, "parse workspace overrides failed, degrading to team defaults", err)
		return nil
	}
	// Valid overrides (or empty): clear any prior warning flag so a future
	// regression is warned again (#749).
	b.warnedOverrides.Delete(workspaceID)
	return overrides
}

// resolveWorkspacePermissionMode returns the effective permission mode tier for a
// session (#789, r3 #804). The returned tier is a CEILING on permissiveness — each
// worker clamps its operator config to never exceed it (codex: most-restrictive of
// session tier vs cfg.Sandbox/ApprovalMode; ACP: auto_approve gated off for tiers
// stricter than bypass), so an injected default can tighten but never escalate.
// Precedence:
//
//   - Sessions WITHOUT a workspace (platform/cron: Slack/Feishu/cron-driven) → "".
//     Each worker applies its OWN default/config (codex honors cfg.Sandbox, ACP
//     honors cfg.AutoApprove; CC/OCS map "" to their own bypass). Injecting a global
//     default here would override an operator's restricted codex.sandbox /
//     acp.auto_approve (#789 P1).
//   - Workspace with an explicit override → that tier wins.
//   - Workspace with NO explicit override, or a fetch error → the bridge's global
//     default (config.worker.default_permission_mode, seeded "workspace" in r3).
//     Returning the default (not "") on fetch error is fail-closed: for CC/OCS ""
//     maps to bypass, so an empty degrade would re-open the r2 P1 escalation on a
//     transient DB outage.
//
// ctx note: GetWorkspaceByID uses shutdownCtx rather than a request-scoped ctx because
// buildWorkerInfo/prepareWorkerInfo intentionally carry no ctx (see #714); the query is
// fast and degrades harmlessly, so request-scoped cancellation isn't propagated here
// (unlike resolveWorkspaceOverrides, whose call sites carry a ctx). #789 review UNCERTAIN.
func (b *Bridge) resolveWorkspacePermissionMode(workspaceID string) string {
	if workspaceID == "" || b.wsStore == nil {
		return ""
	}
	defaultMode, _ := b.defaultPermissionMode.Load().(string)
	ws, err := b.wsStore.GetWorkspaceByID(b.shutdownCtx, workspaceID)
	if err != nil {
		// Fail-closed (#804 r3 review): degrade to the bridge default (workspace)
		// rather than "". For CC/OCS "" maps to bypass, so an empty degrade would
		// silently escalate a workspace session to full bypass during a DB blip.
		b.warnOverrideDegrade(workspaceID, "fetch workspace permission_mode failed, degrading to bridge default", err)
		return defaultMode
	}
	// fetch succeeded: clear any prior degrade flag so a future regression warns again
	// (#749, mirrors resolveWorkspaceOverrides 成功路径的 Delete).
	b.warnedOverrides.Delete(workspaceID)
	if ws.PermissionMode != "" {
		return ws.PermissionMode // explicit workspace override wins
	}
	return defaultMode
}

// warnOverrideDegrade logs a degrading warning at most once per workspaceID per
// process lifetime, preventing log spam under high-crash session loops (#749).
// The warning is re-armed when the workspace later resolves successfully.
func (b *Bridge) warnOverrideDegrade(workspaceID, msg string, err error) {
	if _, loaded := b.warnedOverrides.LoadOrStore(workspaceID, struct{}{}); loaded {
		return
	}
	b.log.Warn("bridge: "+msg, "workspace_id", workspaceID, "err", err)
}

// injectAgentConfig loads agent config files and injects the unified system
// prompt into session info. A no-op when config dir is empty or agent config
// is not configured.
// injectExclude lists file base names to skip; when nil, falls back to the
// platform-level default from the atomic config map.
//
// workspaceOverrides selects the WebChat track (LoadForWorkspace) when non-nil;
// nil selects the Message Channel track (Load with botName). WebChat sessions
// don't select a bot, so botName is intentionally not passed to LoadForWorkspace.
//
// Parameter order note: botName (YAML config name, e.g. "my-bot") comes before
// botID (platform runtime ID, e.g. "U12345") because botName is the primary key
// for agent-config path resolution, while botID is only used for logging.
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botName, botID string, injectExclude []string, workspaceOverrides map[string]string) {
	if b.agentConfigDir == "" {
		return
	}
	// botName is the YAML config name for agent-config path resolution.
	// When empty (single-bot mode / webchat / API), bot-level lookup is skipped
	// and resolution falls through to platform-level automatically.
	injectExclude = b.resolveInjectExclude(platform, injectExclude)
	if unknown := agentconfig.ValidateExcludeList(injectExclude); len(unknown) > 0 {
		b.log.Warn("bridge: inject_exclude contains unknown config files",
			"unknown", unknown, "valid", agentconfig.KnownFiles())
	}
	b.log.Debug("bridge: loading agent config", "dir", b.agentConfigDir, "platform", platform, "bot_name", botName, "bot_id", botID, "exclude", injectExclude, "workspace_overrides", workspaceOverrides != nil)
	var configs *agentconfig.AgentConfigs
	var err error
	if workspaceOverrides != nil {
		configs, err = agentconfig.LoadForWorkspace(b.agentConfigDir, platform, workspaceOverrides, injectExclude...)
	} else {
		configs, err = agentconfig.Load(b.agentConfigDir, platform, botName, injectExclude...)
	}
	if err != nil {
		if errors.Is(err, agentconfig.ErrInvalidBotName) {
			b.log.Error("bridge: agent config rejected",
				"dir", b.agentConfigDir, "platform", platform, "bot_name", botName, "bot_id", botID, "err", err)
		} else {
			b.log.Warn("bridge: agent config load failed",
				"dir", b.agentConfigDir, "platform", platform, "bot_name", botName, "bot_id", botID, "err", err)
		}
		return
	}
	if configs.IsEmpty() {
		b.log.Warn("bridge: agent config empty, no files found",
			"dir", b.agentConfigDir, "platform", platform, "bot_name", botName, "bot_id", botID)
		return
	}

	if prompt := agentconfig.BuildSystemPrompt(configs); prompt != "" {
		info.SystemPrompt = prompt
		b.log.Info("bridge: agent config injected", "prompt_len", len(prompt), "platform", platform, "bot_name", botName, "bot_id", botID)
	} else {
		b.log.Debug("bridge: agent config loaded but prompt empty", "platform", platform, "bot_name", botName, "bot_id", botID)
	}
}

// recordCrashLoop tracks consecutive crashes per session. Returns true if the
// session has exceeded crashLoopMax crashes within crashLoopWindow, indicating
// a crash loop that should abort retries to protect gateway stability.
func (b *Bridge) recordCrashLoop(sessionID string) bool {
	b.crashTrackerMu.Lock()
	defer b.crashTrackerMu.Unlock()

	h, ok := b.crashTracker[sessionID]
	if !ok || time.Since(h.firstSeen) > crashLoopWindow {
		h = &crashHistory{firstSeen: time.Now()}
		b.crashTracker[sessionID] = h
	}
	h.count++

	return h.count > crashLoopMax
}

// resetCrashLoop clears the crash history for a session on successful completion.
func (b *Bridge) resetCrashLoop(sessionID string) {
	b.crashTrackerMu.Lock()
	defer b.crashTrackerMu.Unlock()
	delete(b.crashTracker, sessionID)
}
