package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// waitKillTimeout is the maximum time to wait for Worker.Wait() to return
// after calling Kill(). If Wait() still hasn't returned, forwardEvents
// abandons the wait to prevent permanent goroutine blocking.
const waitKillTimeout = 5 * time.Second

// forwardContext carries per-session mutable state for the event forwarding loop.
// Ownership: exclusively owned by the single forwardEvents goroutine per session.
// No concurrent access — all fields are read/written from that goroutine only,
// except turnTimerFired which uses atomic.Bool for timer callback safety.
type forwardContext struct {
	sessionID      string
	workerType     worker.WorkerType
	sessPlatform   string
	sessOwner      string
	workDir        string
	ctx            context.Context
	startTime      time.Time
	turnStartTime  time.Time
	firstEvent     bool
	doneReceived   bool
	turnText       strings.Builder
	lastError      *events.ErrorData
	pendingError   *events.Envelope
	turnTimerFired atomic.Bool
	turnTimer      *time.Timer
}

// forwardEvents proxies worker events to the hub with seq assignment.
// EVT-004: if msgStore is configured, it appends to the event log on done events.
// AEP-020: after the recv channel closes, calls Worker.Wait() to determine exit
// code and sets DoneData.Success accordingly (non-zero exit = crash = success=false).
// bgCtx returns a background context scoped to the gateway lifecycle.
// Falls back to context.Background() when shutdownCtx is nil (unit tests).
func (b *Bridge) bgCtx() context.Context {
	if b.shutdownCtx != nil {
		return b.shutdownCtx
	}
	return context.Background()
}

func (b *Bridge) forwardEvents(w worker.Worker, sessionID string, opts forwardOpts) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("bridge: panic in forwardEvents", "session_id", sessionID, "panic", r, "stack", string(debug.Stack()))
		}
	}()

	workerType := w.Type()
	b.log.Debug("bridge: forwardEvents goroutine started", "session_id", sessionID, "worker_type", workerType, "resumed", opts.resumed)

	// Capture reset generation at goroutine start to detect stale goroutine after /reset.
	var myResetGen int64
	if rg, ok := w.(worker.ResetGenerationer); ok {
		myResetGen = rg.LoadResetGeneration()
	}

	fc := &forwardContext{
		sessionID:     sessionID,
		workerType:    workerType,
		workDir:       opts.workDir,
		ctx:           opts.ctx,
		startTime:     time.Now(),
		turnStartTime: time.Now(),
		firstEvent:    true,
	}

	if b.collector != nil && b.sm != nil {
		if si, err := b.sm.Get(context.Background(), sessionID); err == nil {
			fc.sessPlatform = si.Platform
			fc.sessOwner = si.OwnerID
			if fc.sessOwner == "" {
				fc.sessOwner = si.UserID
			}
		}
	}

	acc := b.getOrInitAccum(sessionID, opts.workDir, fc.startTime)
	if acc.Generation.Load() == 0 && acc.genInitialized.CompareAndSwap(false, true) {
		gen := int64(1)
		if b.turnsQuerier != nil {
			// Use shutdownCtx as parent: these are short DB reads (<3s) to
			// restore turn counters. opts.ctx is scoped to the inbound request
			// and may expire on slow worker startups. shutdownCtx is cancelled
			// on gateway shutdown, ensuring DB queries don't block graceful stop.
			genCtx, genCancel := context.WithTimeout(b.bgCtx(), 3*time.Second)
			latest, _ := b.turnsQuerier.LatestGeneration(genCtx, sessionID)
			genCancel()
			if latest > 0 {
				gen = latest
			}
		}
		acc.Generation.Store(gen)
	}
	if acc.TurnCount.Load() == 0 && b.turnsQuerier != nil {
		// bgCtx() — same rationale as LatestGeneration above.
		tnCtx, tnCancel := context.WithTimeout(b.bgCtx(), 3*time.Second)
		tn, err := b.turnsQuerier.LatestTurnNum(tnCtx, sessionID, acc.Generation.Load())
		tnCancel()
		if err != nil {
			b.log.Warn("turns: restore turn num", "error", err)
		}
		if tn > 0 {
			acc.TurnCount.Store(int32(tn))
		}
	}

	if b.turnTimeout > 0 {
		fc.turnTimer = time.AfterFunc(b.turnTimeout, func() {
			if !fc.turnTimerFired.CompareAndSwap(false, true) {
				return
			}
			b.log.Warn("bridge: turn timeout exceeded, terminating worker",
				"session_id", sessionID, "worker_type", workerType, "turn_timeout", b.turnTimeout)
			b.sendError(sessionID, events.ErrCodeTurnTimeout, "Turn exceeded %v time limit and was terminated.", b.turnTimeout)
			acc := b.getOrInitAccum(sessionID, fc.workDir, fc.startTime)
			b.captureSyntheticEvent(syntheticTurnParams{
				SessionID:  sessionID,
				Reason:     "turn_timeout",
				Message:    fmt.Sprintf("Turn exceeded %v time limit", b.turnTimeout),
				Source:     eventstore.SourceTimeout,
				Platform:   fc.sessPlatform,
				Owner:      fc.sessOwner,
				Model:      acc.ModelName,
				Generation: acc.Generation.Load(),
				TurnNum:    int(acc.TurnCount.Load()),
			})
			_ = w.Terminate(context.Background())
		})
		defer fc.turnTimer.Stop()
	}

	recvCh := w.Conn().Recv()
	for env := range recvCh {
		b.processForwardedEvent(env, w, opts, fc)
	}

	// Flush buffered error that never reached a retry decision point.
	if fc.pendingError != nil {
		if err := b.hub.SendToSession(context.Background(), fc.pendingError); err != nil {
			b.log.Warn("bridge: flush pending error on exit failed", "session_id", sessionID, "err", err)
		}
		b.captureEvent(sessionID, fc.pendingError.Seq, fc.pendingError.Event.Type, fc.pendingError.Event.Data)
		fc.pendingError = nil
	}

	b.handleWorkerExit(w, workerExitParams{
		sessionID:      sessionID,
		workerType:     workerType,
		opts:           opts,
		startTime:      fc.startTime,
		doneReceived:   fc.doneReceived,
		turnText:       fc.turnText.String(),
		turnTextLen:    fc.turnText.Len(),
		turnTimerFired: fc.turnTimerFired.Load(),
		sessPlatform:   fc.sessPlatform,
		sessOwner:      fc.sessOwner,
		resetGen:       myResetGen,
	})
}

// processForwardedEvent handles a single worker event in the forwarding loop.
func (b *Bridge) processForwardedEvent(env *events.Envelope, w worker.Worker, opts forwardOpts, fc *forwardContext) {
	sessionID := fc.sessionID
	workerType := fc.workerType

	// Refresh worker LastIO on activity (issue #815, spec Fix C). Without this,
	// a long turn (>ExecutionTimeout) is wrongly reaped by the zombie scanner
	// because LastIO stays frozen at the input time — codex only bumped LastIO
	// in Input()/startNewThread, not during the turn. Done (turn terminator) is
	// excluded. SetLastIO lives on base.BaseWorker (not the Worker interface),
	// hence the type assertion; all real workers embed BaseWorker and satisfy it.
	if env.Event.Type != events.Done {
		if lu, ok := w.(interface{ SetLastIO(time.Time) }); ok {
			lu.SetLastIO(time.Now())
		}
	}

	// Handle internal reset events from in-place-reset workers (OCS, ACP).
	if env.Event.Type == events.KindInternalReset {
		b.handleInternalReset(env, sessionID, fc)
		return
	}

	// Buffer error events for potential LLM retry.
	if env.Event.Type == events.Error {
		b.log.Warn("bridge: received error from worker", "session_id", sessionID, "worker_type", workerType, "data", env.Event.Data)
		if ed, ok := env.Event.Data.(events.ErrorData); ok {
			fc.lastError = &ed
		}
		// When LLM retry is active, buffer the error and suppress forwarding.
		// The error is flushed only if retry decides NOT to retry (after Done).
		if b.retryCtrl != nil {
			cloned := events.Clone(env)
			cloned.SessionID = sessionID
			fc.pendingError = cloned
			return
		}
	}

	if fc.firstEvent {
		// Safety-net: persist again on first event in case the post-launch
		// persist was incomplete. Scenarios: (1) GetWorkerSessionID() had not
		// returned a value at launch time; (2) guard re-persist in transitionState
		// failed; (3) gateway crashed between launch and first event.
		b.persistWorkerSessionIDEnsure(opts.ctx, w, sessionID)
		fc.turnStartTime = time.Now()
		fc.firstEvent = false
	}

	// New turn detection: state(running) marks the beginning of a new turn.
	// Reset doneReceived so crash detection applies to this turn (prevents
	// stale true from a previous turn masking a crash in the current turn).
	if env.Event.Type == events.State {
		fc.doneReceived = false
	}

	if fc.turnTimer != nil && !fc.turnTimerFired.Load() {
		fc.turnTimer.Reset(b.turnTimeout)
	}
	if fc.turnTimerFired.Load() {
		return
	}

	env = events.Clone(env)
	env.SessionID = sessionID
	env.OwnerID = fc.sessOwner

	deltaContent, reasoningContent := b.extractTurnContent(env, fc)

	// Stats accumulation.
	b.accumulateStats(env, w, opts, fc)

	// Done processing: mark received, reconcile dropped deltas.
	if env.Event.Type == events.Done {
		fc.doneReceived = true
		b.resetCrashLoop(sessionID)
		b.reconcileDroppedDeltas(env, fc)
		// Messaging sessions transition to IDLE on turn completion (Fix A,
		// issue #815): otherwise they stay RUNNING and get reaped by the 30m
		// zombie ExecutionTimeout, forcing codex into a fresh-start that loses
		// ephemeral-thread context. webchat is excluded — it IDLEs on WS close
		// (conn.go:146), and transitioning here breaks Fast Reconnect.
		b.maybeTransitionIdleAfterDone(sessionID, fc)
	}

	if err := b.hub.SendToSession(context.Background(), env); err != nil {
		b.log.Warn("bridge: forward event failed", "err", err, "session_id", sessionID, "worker_type", workerType, "event_type", env.Event.Type)
	}

	b.captureForwardedEvent(env, deltaContent, reasoningContent, fc)

	// Flush buffered error on non-Done events.
	b.flushPendingError(fc, true)

	// LLM retry: check after Done is forwarded.
	if env.Event.Type == events.Done && b.retryCtrl != nil && (!opts.resumed || fc.turnText.Len() > 0) {
		if shouldRetry, attempt := b.retryCtrl.ShouldRetry(context.TODO(), sessionID, fc.lastError); shouldRetry {
			fc.pendingError = nil
			// Pre-register cancel channel before launching goroutine to close
			// the race window where CancelRetry can't find the channel.
			cancelCh := make(chan struct{})
			b.retryCancelMu.Lock()
			b.retryCancel[sessionID] = cancelCh
			b.retryCancelMu.Unlock()
			// Run autoRetry asynchronously so forwardEvents continues reading
			// from recvCh. This prevents the goroutine from blocking during
			// the backoff period — if the worker crashes, the for-range loop
			// detects recvCh closure immediately instead of waiting for the
			// backoff timer to expire. The goroutine uses shutdownCtx so it
			// cancels promptly during bridge shutdown.
			go b.autoRetry(b.shutdownCtx, w, sessionID, attempt, cancelCh)
			fc.turnText.Reset()
			if b.collector != nil {
				b.collector.ResetSession(sessionID)
			}
			fc.lastError = nil
			return // continue — retry produces new events on recv
		}
		b.flushPendingError(fc, false)
		b.retryCtrl.RecordSuccess(sessionID)
		fc.lastError = nil
	}

	if env.Event.Type == events.Done {
		fc.turnText.Reset()
		fc.turnStartTime = time.Now()
	}

}

// extractTurnContent extracts message/reasoning content for turn tracking.
func (b *Bridge) extractTurnContent(env *events.Envelope, fc *forwardContext) (deltaContent, reasoningContent string) {
	if env.Event.Type == events.MessageDelta || env.Event.Type == events.Message {
		if content := extractMessageContent(env); content != "" {
			fc.turnText.WriteString(content)
			if env.Event.Type == events.MessageDelta {
				deltaContent = content
			}
		}
	} else if env.Event.Type == events.Reasoning {
		reasoningContent = extractReasoningContent(env)
	}
	return
}

func (b *Bridge) handleInternalReset(env *events.Envelope, sessionID string, fc *forwardContext) {
	var data events.InternalResetData
	switch d := env.Event.Data.(type) {
	case events.InternalResetData:
		data = d
	case map[string]any:
		gen, exists := d["generation"]
		if !exists {
			return
		}
		switch v := gen.(type) {
		case int64:
			data = events.InternalResetData{Generation: v}
		case float64:
			data = events.InternalResetData{Generation: int64(v)}
		case json.Number:
			if n, err := v.Int64(); err == nil {
				data = events.InternalResetData{Generation: n}
			} else {
				return
			}
		default:
			return
		}
	default:
		return
	}
	acc := b.getOrInitAccum(sessionID, fc.workDir, fc.startTime)
	if acc.AppliedResetGen.Load() < data.Generation {
		acc.Generation.Add(1)
		acc.AppliedResetGen.Store(data.Generation)
	}
	acc.TurnCount.Store(0)
	fc.turnText.Reset()
}

// accumulateStats tracks tool calls and per-turn stats on done events.
func (b *Bridge) accumulateStats(env *events.Envelope, w worker.Worker, opts forwardOpts, fc *forwardContext) {
	sessionID := fc.sessionID

	switch env.Event.Type {
	case events.ToolCall:
		acc := b.getOrInitAccum(sessionID, fc.workDir, fc.startTime)
		acc.ToolCallCount.Add(1)
		if tc, ok := asToolCallData(env.Event.Data); ok {
			if acc.ToolNames == nil {
				acc.ToolNames = make(map[string]int)
			}
			acc.ToolNames[tc.Name]++
		}
	case events.Done:
		if fc.turnTimer != nil {
			fc.turnTimer.Stop()
		}
		acc := b.getOrInitAccum(sessionID, opts.workDir, fc.startTime)
		if dd, ok := asDoneData(env.Event.Data); ok {
			acc.mergePerTurnStats(dd)
		}
		acc.TurnCount.Add(1)
		acc.TurnDurationMs = time.Since(fc.turnStartTime).Milliseconds()
		acc.computePerTurnDeltas()

		if cr, ok := w.(worker.ControlRequester); ok {
			fetchContextUsage(cr, acc)
		}

		b.injectSessionStats(env, acc)
		b.captureAssistantTurn(sessionID, env.Seq, acc, fc.turnText.String(),
			fc.sessOwner, fc.sessPlatform, env.Timestamp)
		acc.resetPerTurn()
		if b.log.Enabled(context.Background(), slog.LevelDebug) {
			b.log.Debug("bridge: turn completed",
				"session_id", sessionID, "worker_type", fc.workerType, "turn", acc.TurnCount.Load(),
				"duration", time.Since(fc.turnStartTime).Round(time.Millisecond),
				"text_len", fc.turnText.Len(), "tools", acc.ToolCallCount.Load())
		}
	}
}

// maybeTransitionIdleAfterDone transitions a messaging session to IDLE after
// the turn completes (Fix A, issue #815). Without this, messaging sessions
// remain RUNNING and are reaped by the 30m zombie ExecutionTimeout, forcing
// codex into a fresh-start that loses ephemeral-thread context.
//
// webchat is excluded: it transitions to IDLE on WS close (conn.go:146), and
// transitioning here would break Fast Reconnect (which requires State==Running
// per session.md). Empty platform (collector disabled / unidentified) is skipped
// conservatively to avoid IDLE-ing sessions we can't classify.
func (b *Bridge) maybeTransitionIdleAfterDone(sessionID string, fc *forwardContext) {
	if b.sm == nil || fc.sessPlatform == "" || fc.sessPlatform == platformWebChat {
		return
	}
	if err := b.sm.Transition(context.Background(), sessionID, events.StateIdle); err != nil {
		b.log.Debug("bridge: post-done transition to idle rejected",
			"session_id", sessionID, "platform", fc.sessPlatform, "err", err)
	}
}

// reconcileDroppedDeltas marks the done event when deltas were dropped under backpressure.
func (b *Bridge) reconcileDroppedDeltas(env *events.Envelope, fc *forwardContext) {
	if !b.hub.GetAndClearDropped(fc.sessionID) {
		return
	}
	b.log.Warn("bridge: handling dropped deltas before done", "session_id", fc.sessionID, "worker_type", fc.workerType)

	if dataMap, ok := env.Event.Data.(map[string]any); ok {
		if stats, ok := dataMap["stats"].(map[string]any); ok {
			stats["dropped"] = true
		} else {
			dataMap["stats"] = map[string]any{"dropped": true}
		}
	} else if doneData, ok := env.Event.Data.(events.DoneData); ok {
		doneData.Dropped = true
		env.Event.Data = doneData
	} else if doneDataPtr, ok := env.Event.Data.(*events.DoneData); ok {
		doneDataPtr.Dropped = true
		env.Event.Data = doneDataPtr
	}
}

// captureForwardedEvent persists the event for replay.
func (b *Bridge) captureForwardedEvent(env *events.Envelope, deltaContent, reasoningContent string, fc *forwardContext) {
	sessionID := fc.sessionID
	if deltaContent != "" && b.collector != nil {
		b.collector.CaptureDeltaString(sessionID, env.Seq, deltaContent)
	} else if reasoningContent != "" && b.collector != nil {
		b.collector.CaptureReasoningString(sessionID, env.Seq, reasoningContent)
	} else if env.Event.Type != events.MessageDelta && env.Event.Type != events.Reasoning {
		b.captureEvent(sessionID, env.Seq, env.Event.Type, env.Event.Data)
	}
}

// flushPendingError sends the buffered error event to the client.
// skipOnDone controls whether to suppress the flush when the current event is Done
// (used in the main forwarding loop to defer error delivery past retry decision).
func (b *Bridge) flushPendingError(fc *forwardContext, skipOnDone bool) {
	if fc.pendingError == nil {
		return
	}
	if skipOnDone && fc.doneReceived {
		return
	}
	if err := b.hub.SendToSession(context.Background(), fc.pendingError); err != nil {
		b.log.Warn("bridge: forward buffered error failed", "err", err, "session_id", fc.sessionID, "worker_type", fc.workerType)
	}
	b.captureEvent(fc.sessionID, fc.pendingError.Seq, fc.pendingError.Event.Type, fc.pendingError.Event.Data)
	fc.pendingError = nil
}

// workerExitParams carries the context needed by handleWorkerExit.
type workerExitParams struct {
	sessionID      string
	workerType     worker.WorkerType
	opts           forwardOpts
	startTime      time.Time
	doneReceived   bool
	turnText       string
	turnTextLen    int
	turnTimerFired bool
	sessPlatform   string
	sessOwner      string
	// resetGen is the resetGeneration captured when forwardEvents started.
	// If the current generation differs, another reset replaced this goroutine's
	// worker and we must NOT cleanupCrashedWorker (would detach the NEW worker).
	resetGen int64
}

// handleWorkerExit processes worker exit after the recv channel closes.
// It determines the exit code, attempts crash recovery, sends error events,
// and performs cleanup.
func (b *Bridge) handleWorkerExit(w worker.Worker, p workerExitParams) {
	workerType := p.workerType

	// P0 guard: if the worker was reset (generation changed), a NEW forwardEvents
	// goroutine is already managing the replacement worker. This OLD goroutine must
	// exit silently — cleanupCrashedWorker would detach the NEW worker and delete
	// the accumulator, breaking the session.
	if rg, ok := w.(worker.ResetGenerationer); ok && rg.LoadResetGeneration() != p.resetGen {
		b.log.Info("bridge: worker exit from stale forwardEvents after reset, skipping cleanup",
			"session_id", p.sessionID, "worker_type", workerType,
			"my_gen", p.resetGen, "current_gen", rg.LoadResetGeneration())
		return
	}

	// AEP-020: Worker.Recv() closed — get exit code to determine crash vs normal exit.
	// Must match proc.DefaultGracePeriod (5s) so SIGTERM grace isn't cut short.
	waitTimeout := 5 * time.Second
	if b.closed.Load() {
		waitTimeout = 10 * time.Second
	}
	var exitCode int
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("bridge: panic in waitWorker", "session_id", p.sessionID, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		exitCode, _ = w.Wait()
	}()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		b.log.Warn("bridge: Wait() timed out, force-killing", "session_id", p.sessionID, "worker_type", workerType)
		_ = w.Kill()
		// Secondary timeout: if Wait() still doesn't return after Kill()
		// (e.g., Go runtime deadlock or zombie process), abandon the wait
		// instead of blocking forwardEvents forever.
		killTimeout := time.NewTimer(waitKillTimeout)
		defer killTimeout.Stop()
		select {
		case <-ch:
			// Wait completed after Kill.
		case <-killTimeout.C:
			b.log.Warn("bridge: Wait() did not return after Kill(), abandoning",
				"session_id", p.sessionID, "worker_type", workerType)
		}
	}

	// Crash recovery retry: attempt when the worker exited without completing
	// its turn (no "done" received). Skip during shutdown, SIGTERM (143),
	// Applies to both fresh and resumed sessions — Resume() gracefully falls back
	// to fresh Start() for workers that cannot preserve conversation history.
	fallbackAttempted := b.sm != nil && !b.closed.Load() && !p.doneReceived && exitCode != 143 && exitCode != -1 && p.opts.retryDepth < 2 && time.Since(p.startTime) < 15*time.Second
	if fallbackAttempted && p.turnTextLen == 0 && time.Since(p.startTime) < 5*time.Second {
		b.log.Info("bridge: session files missing after resume, skipping retry",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode)
		p.opts.retryDepth = 1
	}
	if fallbackAttempted {
		var lastInput string
		if conn := w.Conn(); conn != nil {
			if ir, ok := conn.(worker.InputRecoverer); ok {
				lastInput = sanitizeLastInput(ir.LastInput())
			}
		}
		if lastInput == "" {
			lastInput = p.opts.lastInput
		}
		acc := b.getOrInitAccum(p.sessionID, "", p.startTime)
		if b.attemptResumeFallback(fallbackParams{
			sessionID:     p.sessionID,
			workDir:       p.opts.workDir,
			exitCode:      exitCode,
			retryDepth:    p.opts.retryDepth,
			workerType:    workerType,
			lastInput:     lastInput,
			crashedWorker: w,
			sessPlatform:  p.sessPlatform,
			sessOwner:     p.sessOwner,
			accGeneration: acc.Generation.Load(),
			accModelName:  acc.ModelName,
		}) {
			return
		}
	}

	if b.closed.Load() {
		b.cleanupCrashedWorker(p.sessionID, w)
		return
	}

	if b.sm != nil {
		si, smErr := b.sm.Get(context.Background(), p.sessionID)
		if smErr == nil && si.State == events.StateTerminated {
			b.log.Debug("bridge: session already terminated, skipping error for handler-killed worker", "session_id", p.sessionID, "worker_type", workerType)
			if !fallbackAttempted {
				b.cleanupCrashedWorker(p.sessionID, w)
			}
			return
		}
	}

	// Suppress user-facing errors when:
	// 1. Session completed normally: "done" received with no pending turn text.
	// 2. Worker was intentionally terminated: SIGTERM (exit 143) is always
	//    bridge/handler/GC-initiated, never an unexpected crash.
	if p.doneReceived && p.turnTextLen == 0 {
		b.log.Info("bridge: worker exit clean (done received, no pending output)",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode)
	} else if exitCode == 143 {
		b.log.Info("bridge: worker exit intentional (SIGTERM)",
			"session_id", p.sessionID, "worker_type", workerType)
	} else if exitCode != 0 && exitCode != -1 {
		acc := b.getOrInitAccum(p.sessionID, "", p.startTime)
		b.log.Warn("bridge: worker exited with non-zero code, sending crash error",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode,
			"duration", time.Since(p.startTime).Round(time.Millisecond), "turn_count", acc.TurnCount.Load())
		observability.WorkerCrashes().Add(context.TODO(), 1, metric.WithAttributes(attribute.String("worker_type", string(workerType)), attribute.String("exit_code", fmt.Sprintf("%d", exitCode))))
		b.sendError(p.sessionID, events.ErrCodeWorkerCrash, "worker crashed (exit code %d)", exitCode)
		b.captureSyntheticEvent(syntheticTurnParams{
			SessionID:  p.sessionID,
			Reason:     "worker_crash",
			Message:    fmt.Sprintf("Worker crashed with exit code %d", exitCode),
			Source:     eventstore.SourceCrash,
			Platform:   p.sessPlatform,
			Owner:      p.sessOwner,
			Model:      acc.ModelName,
			Generation: acc.Generation.Load(),
			TurnNum:    int(acc.TurnCount.Load()),
		})
	} else if exitCode == -1 {
		b.sendError(p.sessionID, events.ErrCodeSessionTerminated, "worker terminated (killed)")
	} else if !p.doneReceived {
		b.log.Debug("bridge: sending error for platform cleanup (no done received)", "session_id", p.sessionID, "worker_type", workerType)
		b.sendError(p.sessionID, events.ErrCodeWorkerCrash, "worker exited without sending done")
	}

	if !fallbackAttempted {
		b.cleanupCrashedWorker(p.sessionID, w)
	}
}

// captureEvent persists an outbound event for replay.
func (b *Bridge) captureEvent(sessionID string, seq int64, eventType events.Kind, data any) {
	b.captureDirected(sessionID, seq, eventType, data, "outbound")
}

// CaptureInboundEvent persists an inbound event for replay only (no turn write).
// Used for interaction responses (permission/question/elicitation) which are not user turns.
func (b *Bridge) CaptureInboundEvent(sessionID string, seq int64, eventType events.Kind, data any) {
	b.captureDirected(sessionID, seq, eventType, data, "inbound")
}

// CaptureInbound persists an inbound (user→worker) event for replay.
// Also writes a user turn record when eventType is Input.
func (b *Bridge) CaptureInbound(ctx context.Context, sessionID string, seq int64, eventType events.Kind, data any, platform, owner string) {
	b.captureDirected(sessionID, seq, eventType, data, "inbound")

	// Write user turn record for Input events.
	if eventType == events.Input && b.collector != nil {
		acc := b.getOrInitAccum(sessionID, "", time.Now())
		// Synchronous Generation initialization to prevent race (#658):
		// forwardEvents initializes acc.Generation asynchronously, but CaptureInbound
		// may be called from the Handler goroutine before that init completes.
		// Without this guard, user turns get Generation=0 while assistant turns get Generation=1+,
		// making the first user turn invisible after page refresh.
		if acc.Generation.Load() == 0 && acc.genInitialized.CompareAndSwap(false, true) {
			gen := int64(1)
			if b.turnsQuerier != nil {
				// shutdownCtx — same rationale as forwardEvents LatestGeneration.
				genCtx, genCancel := context.WithTimeout(b.bgCtx(), 3*time.Second)
				latest, _ := b.turnsQuerier.LatestGeneration(genCtx, sessionID)
				genCancel()
				if latest > 0 {
					gen = latest
				}
			}
			acc.Generation.Store(gen)
		}
		content := extractInputContent(data)
		turn := &eventstore.TurnWriteRequest{
			SessionID:  sessionID,
			Generation: acc.Generation.Load(),
			TurnNum:    int(acc.TurnCount.Load()) + 1,
			Seq:        seq,
			Role:       eventstore.RoleUser,
			Content:    content,
			Platform:   platform,
			UserID:     owner,
			Source:     eventstore.SourceNormal,
			CreatedAt:  time.Now().UnixMilli(),
		}
		b.collector.CaptureTurn(turn)
	}
}

// captureDirected marshals event data and sends it to the collector with the given direction.
func (b *Bridge) captureDirected(sessionID string, seq int64, eventType events.Kind, data any, direction string) {
	if b.collector == nil {
		b.log.Debug("bridge: capture skipped, collector is nil", "session_id", sessionID, "type", eventType, "direction", direction)
		return
	}
	ed, err := json.Marshal(data)
	if err != nil {
		b.log.Warn("bridge: capture marshal failed", "session_id", sessionID, "type", eventType, "direction", direction, "err", err)
		return
	}
	if eventType == events.ToolResult {
		ed = truncateToolResultOutput(ed)
	}
	b.collector.Capture(sessionID, seq, eventType, ed, direction, eventstore.SourceNormal)
}

const maxToolResultOutputLen = 128

// truncateToolResultOutput truncates the output field in a tool_result JSON payload.
func truncateToolResultOutput(raw json.RawMessage) json.RawMessage {
	var v struct {
		ID     string `json:"id"`
		Output any    `json:"output"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	s, ok := v.Output.(string)
	if !ok || utf8.RuneCountInString(s) <= maxToolResultOutputLen {
		return raw
	}
	runes := []rune(s)
	v.Output = string(runes[:maxToolResultOutputLen])
	truncated, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return truncated
}

// syntheticTurnParams carries metadata for writing a synthetic turn (crash/timeout/fresh_start).
type syntheticTurnParams struct {
	SessionID  string
	Reason     string
	Message    string
	Source     string
	Platform   string
	Owner      string
	Model      string
	Generation int64
	TurnNum    int
}

// captureSyntheticEvent writes a synthetic done-like event for crash/timeout/fresh_start scenarios.
// Allocates a real seq number to avoid colliding with the AEP "unassigned" convention (seq=0).
func (b *Bridge) captureSyntheticEvent(p syntheticTurnParams) {
	if b.collector == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"success":   false,
		"reason":    p.Reason,
		"message":   p.Message,
		"synthetic": true,
	})
	if err != nil {
		return
	}
	seq := b.hub.NextSeq(p.SessionID)
	b.collector.Capture(p.SessionID, seq, events.Done, data, "outbound", p.Source)

	sFalse := false
	turn := &eventstore.TurnWriteRequest{
		SessionID:  p.SessionID,
		Generation: p.Generation,
		TurnNum:    p.TurnNum,
		Seq:        seq,
		Role:       eventstore.RoleAssistant,
		Content:    p.Message,
		Platform:   p.Platform,
		UserID:     p.Owner,
		Model:      p.Model,
		Source:     p.Source,
		Success:    &sFalse,
		CreatedAt:  time.Now().UnixMilli(),
	}
	b.collector.CaptureTurn(turn)
}

// captureAssistantTurn writes an assistant turn record from the done event path.
func (b *Bridge) captureAssistantTurn(sessionID string, seq int64, acc *sessionAccumulator, content, owner, platform string, timestamp int64) {
	if b.collector == nil {
		return
	}
	var toolsJSON string
	if len(acc.ToolNames) > 0 {
		b, _ := json.Marshal(acc.ToolNames)
		toolsJSON = string(b)
	}
	tokensInput := max(acc.PerTurnInput-acc.PerTurnCacheWrite-acc.PerTurnCacheRead, 0)
	s := true // Normal completion path is always success.
	success := &s

	turn := &eventstore.TurnWriteRequest{
		SessionID:        sessionID,
		Generation:       acc.Generation.Load(),
		TurnNum:          int(acc.TurnCount.Load()),
		Seq:              seq,
		Role:             eventstore.RoleAssistant,
		Content:          content,
		Platform:         platform,
		UserID:           owner,
		Model:            acc.ModelName,
		Success:          success,
		Source:           eventstore.SourceNormal,
		ToolsJSON:        toolsJSON,
		ToolCount:        int(acc.ToolCallCount.Load()),
		TokensInput:      tokensInput,
		TokensCacheWrite: acc.PerTurnCacheWrite,
		TokensCacheRead:  acc.PerTurnCacheRead,
		TokensOut:        acc.PerTurnOutput,
		DurationMs:       acc.TurnDurationMs,
		CostUSD:          acc.PerTurnCost,
		CreatedAt:        timestamp,
	}
	b.collector.CaptureTurn(turn)
}

// extractInputContent extracts user input text from event data.
func extractInputContent(data any) string {
	switch d := data.(type) {
	case events.InputData:
		return d.Content
	case map[string]any:
		if c, ok := d["content"].(string); ok {
			return c
		}
	}
	return ""
}

// extractMessageContent extracts text content from a message or message_delta event.
func extractMessageContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.Message, events.MessageDelta:
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
			return d.Content
		}
		if m, ok := env.Event.Data.(map[string]any); ok {
			if content, ok := m["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}

// extractReasoningContent extracts text content from a reasoning event.
func extractReasoningContent(env *events.Envelope) string {
	if env.Event.Type != events.Reasoning {
		return ""
	}
	if d, ok := env.Event.Data.(events.ReasoningData); ok {
		return d.Content
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			return content
		}
	}
	return ""
}

// getOrInitAccum returns the session accumulator, creating one if needed.
// gitBranchOf is called outside the lock to avoid blocking all sessions
// during the ~2s git subprocess. The branch is set on the accumulator
// after re-acquiring the lock.
func (b *Bridge) getOrInitAccum(sessionID, workDir string, startTime time.Time) *sessionAccumulator {
	// Fast path: check if accumulator exists under read lock.
	b.accumMu.RLock()
	acc, ok := b.accum[sessionID]
	needsWorkDir := ok && workDir != "" && acc.WorkDir == ""
	b.accumMu.RUnlock()
	if ok {
		if needsWorkDir {
			// Resolve git branch outside any lock (up to 2s subprocess).
			branch := gitBranchOf(workDir)
			b.accumMu.Lock()
			acc.WorkDir = workDir
			acc.GitBranch = branch
			b.accumMu.Unlock()
		}
		return acc
	}

	// Slow path: resolve git branch before acquiring write lock.
	var branch string
	if workDir != "" {
		branch = gitBranchOf(workDir)
	}

	b.accumMu.Lock()
	// Double-check after acquiring write lock.
	if acc, ok := b.accum[sessionID]; ok {
		if workDir != "" && acc.WorkDir == "" {
			acc.WorkDir = workDir
			acc.GitBranch = branch
		}
		b.accumMu.Unlock()
		return acc
	}
	acc = &sessionAccumulator{
		StartedAt: startTime,
		WorkDir:   workDir,
		GitBranch: branch,
	}
	b.accum[sessionID] = acc
	b.accumMu.Unlock()
	return acc
}

// injectSessionStats merges the accumulator snapshot into DoneData.Stats["_session"].
// Handles both typed DoneData and map[string]any (from events.Clone JSON round-tripping).
func (b *Bridge) injectSessionStats(env *events.Envelope, acc *sessionAccumulator) {
	dd, ok := asDoneData(env.Event.Data)
	if !ok {
		return
	}
	if dd.Stats == nil {
		dd.Stats = make(map[string]any)
	}
	dd.Stats["_session"] = acc.snapshot()

	// Write back: preserve the original representation (map stays map, struct stays struct).
	switch env.Event.Data.(type) {
	case map[string]any:
		raw, _ := json.Marshal(dd)
		_ = json.Unmarshal(raw, &env.Event.Data)
	default:
		env.Event.Data = dd
	}
}

// gitBranchOf delegates to messaging.GitBranchOf for branch resolution.
func gitBranchOf(dir string) string { return messaging.GitBranchOf(dir) }

// fetchContextUsage queries the worker for precise context usage via control channel.
// Errors are silently ignored; the caller falls back to aggregated Done stats.
func fetchContextUsage(cr worker.ControlRequester, acc *sessionAccumulator) {
	ctrlCtx, ctrlCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctrlCancel()
	if resp, err := cr.SendControlRequest(ctrlCtx, "get_context_usage", nil); err == nil {
		if cu := events.MapContextUsageResponse(resp); cu.MaxTokens > 0 || cu.TotalTokens > 0 || cu.Model != "" {
			acc.mergeContextUsage(cu)
		}
	}
}
