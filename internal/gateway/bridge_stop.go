package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

var (
	errWorkerRunChanged     = errors.New("worker run changed")
	errWorkerEventBarrier   = errors.New("worker event barrier timeout")
	errWorkerRunTerminal    = errors.New("worker run turn already terminal")
	errWorkerStopNotApplied = errors.New("worker stop not applied")
	errWorkerRunTeardown    = errors.New("worker run teardown failed")
	errWorkerRunQuiescence  = errors.New("worker run quiescence timeout")
)

// StopAndDisposeCurrentRun stops and releases exactly one local Worker run.
// A nil result proves that the provider cancellation committed, the old event
// stream is permanently fenced, and the forwarder completed its CAS cleanup.
func (b *Bridge) StopAndDisposeCurrentRun(ctx context.Context, sessionID, expectedRunID string) error {
	started := time.Now()
	binding, ok := b.currentWorkerRunBinding(sessionID, expectedRunID)
	if !ok || binding.lifecycle == nil {
		b.logStopPhase(sessionID, expectedRunID, "stop_failed", started, "run_changed", "")
		return errWorkerRunChanged
	}
	workerType := binding.worker.Type()

	teardownTimeout := b.stopTeardownTimeout
	if teardownTimeout <= 0 {
		teardownTimeout = defaultStopTeardownTimeout
	}
	forwarderTimeout := b.stopForwarderTimeout
	if forwarderTimeout <= 0 {
		forwarderTimeout = defaultStopForwarderTimeout
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
	defer cancel()

	b.logStopPhase(sessionID, binding.id, "stop_requested", started, "", workerType)
	lifecycle := binding.lifecycle
	if !lifecycle.lockEventBarrier(stopCtx) {
		b.logStopPhase(sessionID, binding.id, "stop_failed", started, "event_barrier", workerType)
		return errWorkerEventBarrier
	}
	current, stillCurrent := b.currentWorkerRunBinding(sessionID, expectedRunID)
	if !stillCurrent || current.worker != binding.worker || current.lifecycle != lifecycle {
		lifecycle.unlockEventBarrier()
		b.logStopPhase(sessionID, binding.id, "stop_failed", started, "run_changed", workerType)
		return errWorkerRunChanged
	}
	if lifecycle.terminalCommitted.Load() {
		lifecycle.unlockEventBarrier()
		b.logStopPhase(sessionID, binding.id, "stop_completed", started, "already_terminal", workerType)
		return errWorkerRunTerminal
	}
	if err := stopCurrentTurnUnderEventBarrier(lifecycle, func() error {
		return binding.worker.StopCurrentTurn(stopCtx)
	}); err != nil {
		b.logStopPhase(sessionID, binding.id, "stop_failed", started, "provider_cancel", workerType)
		return err
	}
	b.logStopPhase(sessionID, binding.id, "provider_cancelled", started, "", workerType)

	b.logStopPhase(sessionID, binding.id, "worker_terminating", started, "", workerType)
	teardownFailed := false
	if err := invokeWorkerTeardown(func() error { return binding.worker.Terminate(stopCtx) }); err != nil {
		b.logStopPhase(sessionID, binding.id, "worker_kill_fallback", started, "terminate", workerType)
		if killErr := invokeWorkerTeardown(binding.worker.Kill); killErr != nil {
			teardownFailed = true
			b.logStopPhase(sessionID, binding.id, "stop_failed", started, "kill", workerType)
		}
	}
	if lifecycle.conn != nil {
		if err := invokeWorkerTeardown(lifecycle.conn.Close); err != nil {
			b.logStopPhase(sessionID, binding.id, "stop_failed", started, "conn_close", workerType)
		}
	}

	if !waitForWorkerRun(lifecycle.done, stopCtx.Done(), forwarderTimeout) {
		b.failClosedStoppedRun(sessionID, binding)
		b.logStopPhase(sessionID, binding.id, "stop_failed", started, "quiescence_timeout", workerType)
		return errWorkerRunQuiescence
	}
	b.logStopPhase(sessionID, binding.id, "forwarder_quiesced", started, "", workerType)

	if b.workerRunStillAttached(sessionID, binding) {
		b.failClosedStoppedRun(sessionID, binding)
		b.logStopPhase(sessionID, binding.id, "stop_failed", started, "cleanup_incomplete", workerType)
		return errWorkerRunQuiescence
	}
	if teardownFailed {
		return errWorkerRunTeardown
	}
	b.logStopPhase(sessionID, binding.id, "stop_completed", started, "", workerType)
	return nil
}

// beginWorkerRunTurn reopens the per-turn terminal state only after every
// event admitted for the preceding turn has completed. The caller holds the
// session dispatch gate, so a stop cannot observe the reopened state before
// the new provider dispatch begins.
func (b *Bridge) beginWorkerRunTurn(ctx context.Context, sessionID, expectedRunID string) error {
	binding, ok := b.currentWorkerRunBinding(sessionID, expectedRunID)
	if !ok || binding.lifecycle == nil {
		return errWorkerRunChanged
	}
	lifecycle := binding.lifecycle
	if !lifecycle.lockEventBarrier(ctx) {
		return errWorkerEventBarrier
	}
	defer lifecycle.unlockEventBarrier()

	current, stillCurrent := b.currentWorkerRunBinding(sessionID, expectedRunID)
	if !stillCurrent || current.worker != binding.worker || current.lifecycle != lifecycle {
		return errWorkerRunChanged
	}
	lifecycle.terminalCommitted.Store(false)
	return nil
}

func stopCurrentTurnUnderEventBarrier(lifecycle *workerRunLifecycle, stop func() error) (err error) {
	defer lifecycle.unlockEventBarrier()
	defer func() {
		if recover() != nil {
			err = errWorkerStopNotApplied
		}
	}()
	if err := stop(); err != nil {
		return errWorkerStopNotApplied
	}
	lifecycle.stopping.Store(true)
	return nil
}

func invokeWorkerTeardown(teardown func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = errWorkerRunTeardown
		}
	}()
	return teardown()
}

func (b *Bridge) currentWorkerRunBinding(sessionID, expectedRunID string) (workerRunBinding, bool) {
	value, ok := b.workerRuns.Load(sessionID)
	if !ok {
		return workerRunBinding{}, false
	}
	binding, ok := value.(workerRunBinding)
	if !ok || binding.worker == nil || binding.id == "" || (expectedRunID != "" && binding.id != expectedRunID) {
		return workerRunBinding{}, false
	}
	if b.sm == nil || b.sm.GetWorker(sessionID) != binding.worker {
		return workerRunBinding{}, false
	}
	return binding, true
}

func waitForWorkerRun(done, teardownDone <-chan struct{}, forwarderTimeout time.Duration) bool {
	select {
	case <-done:
		return true
	case <-teardownDone:
	}

	timer := time.NewTimer(forwarderTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (b *Bridge) failClosedStoppedRun(sessionID string, binding workerRunBinding) {
	detached := b.sm != nil && b.sm.DetachWorkerIf(sessionID, binding.worker)
	b.clearWorkerRun(sessionID, binding.worker, binding.id)
	if !detached {
		return
	}
	if err := b.sm.Transition(context.Background(), sessionID, events.StateIdle); err != nil {
		b.log.Debug("bridge: fail-closed stopped run idle transition failed",
			"session_id", sessionID, "worker_run_id", binding.id, "error_kind", "idle_transition")
	}
}

func (b *Bridge) workerRunStillAttached(sessionID string, binding workerRunBinding) bool {
	if b.sm != nil && b.sm.GetWorker(sessionID) == binding.worker {
		return true
	}
	value, ok := b.workerRuns.Load(sessionID)
	if !ok {
		return false
	}
	current, ok := value.(workerRunBinding)
	return ok && current.worker == binding.worker && current.id == binding.id && current.lifecycle == binding.lifecycle
}

func (b *Bridge) logStopPhase(sessionID, runID, phase string, started time.Time, errorKind string, workerType worker.WorkerType) {
	attrs := []any{
		"session_id", sessionID,
		"worker_run_id", runID,
		"stop_phase", phase,
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if workerType != "" {
		attrs = append(attrs, "worker_type", workerType)
	}
	if errorKind != "" {
		attrs = append(attrs, "error_kind", errorKind)
	}
	b.log.Info("bridge: worker run stop phase", attrs...)
}

func stopFailureAllowsRollback(err error) bool {
	return errors.Is(err, errWorkerRunChanged) ||
		errors.Is(err, errWorkerEventBarrier) ||
		errors.Is(err, errWorkerRunTerminal) ||
		errors.Is(err, errWorkerStopNotApplied)
}

func stopErrorKind(err error) string {
	switch {
	case errors.Is(err, errWorkerRunChanged):
		return "run_changed"
	case errors.Is(err, errWorkerEventBarrier):
		return "event_barrier"
	case errors.Is(err, errWorkerRunTerminal):
		return "already_terminal"
	case errors.Is(err, errWorkerStopNotApplied):
		return "provider_cancel"
	case errors.Is(err, errWorkerRunTeardown):
		return "teardown"
	case errors.Is(err, errWorkerRunQuiescence):
		return "quiescence_timeout"
	default:
		return "unknown"
	}
}
