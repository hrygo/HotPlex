package gateway

import "sync"

// turnStopFence is the per-turn single-stop admission fence for control.stop
// (Task 5: Gateway stop/next-turn 单终态合同).
//
// Contract: at most ONE effective StopCurrentTurn per (session, worker run,
// turn execution). The claim is keyed by the composite of the worker run ID
// and the turn's execution ID (from the execution ledger). Because each new
// primary turn accepts a NEW execution record, a new turn's stop is always
// admitted under its own key — a racing input can never clear an in-flight
// stop's claim (BeginTurn with a different execution ID has no matching
// entry), and a double-click/retried stop for the same turn hits the same
// composite and is admitted=false: no second Worker call, no second Done, no
// second runtime finish. When the execution ledger is disabled (executionID
// empty), the fence degrades to today's (session, run) semantics so test-mode
// behavior is preserved. StopCurrentTurn failures release the claim (Rollback)
// so a manual retry can stop again; a successful stop keeps the claim for the
// turn. Sessions that are stopped and then deleted/GC'd release their claim
// via Delete (never evicted by input paths alone).
//
// Zero value is ready to use: the claimed map is lazily initialized under the
// lock, so handlers constructed without NewHandler (unit tests) stay safe.
//
// Explicit mu field per project convention (no embedding, no pointer passing).
type turnStopFence struct {
	mu      sync.Mutex
	claimed map[string]string // session ID -> composite (runID + "\x00" + execID)
}

// stopClaimKey builds the composite claim key. \x00 cannot appear in run IDs
// (run_<uuid>) or execution IDs (uuid), so it is a safe field separator.
func stopClaimKey(workerRunID, execID string) string {
	return workerRunID + "\x00" + execID
}

// Claim admits the stop for sessionID/workerRunID/execID. It returns true only
// for the first caller; a duplicate claim for the same session/run/execution
// returns false (the gateway must not call StopCurrentTurn again). A claim for
// the same session with a DIFFERENT run or execution (worker replaced, or a
// new turn on the same worker) is admitted and overwrites the stale entry.
func (f *turnStopFence) Claim(sessionID, workerRunID, execID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed == nil {
		f.claimed = make(map[string]string)
	}
	key := stopClaimKey(workerRunID, execID)
	if prev, ok := f.claimed[sessionID]; ok && prev == key {
		return false
	}
	f.claimed[sessionID] = key
	return true
}

// Matches reports whether the retained claim belongs to the exact run and
// execution. Detached-run idempotence must use this whenever either identity
// is known; a stale claim from an older turn is not evidence that the latest
// turn was stopped.
func (f *turnStopFence) Matches(sessionID, workerRunID, execID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	claim, ok := f.claimed[sessionID]
	return ok && claim == stopClaimKey(workerRunID, execID)
}

// HasAny is the identity-free fallback for ledger-disabled operation after a
// detached run leaves no live run ID. Configured-ledger lookup failures must
// fail closed rather than call this method.
func (f *turnStopFence) HasAny(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.claimed[sessionID]
	return ok
}

// Rollback releases the claim after a FAILED StopCurrentTurn so a manual retry
// can stop again. It only clears when the claim still belongs to the given
// run/execution — an old run's rollback must not clear a newer claim.
func (f *turnStopFence) Rollback(sessionID, workerRunID, execID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := stopClaimKey(workerRunID, execID)
	if prev, ok := f.claimed[sessionID]; ok && prev == key {
		delete(f.claimed, sessionID)
	}
}

// BeginTurn clears a stop claim for the session before a new primary turn
// (handleInput, after worker binding / MarkRunning, before the new primary
// w.Input). The clear is scoped to the same session, run, AND execution: a new
// turn carries a new execution ID, so its BeginTurn cannot delete the previous
// turn's claim while a stop for it is still in flight (the single-stop fence
// must survive the input path racing the stop path). The clear is meaningful
// only when the execution ledger is disabled (empty execID) — then it restores
// the original same-session/same-run semantics so a later turn can stop again
// even when the same worker run spans turns.
func (f *turnStopFence) BeginTurn(sessionID, workerRunID, execID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := stopClaimKey(workerRunID, execID)
	if prev, ok := f.claimed[sessionID]; ok && prev == key {
		delete(f.claimed, sessionID)
	}
}

// Delete removes any claim for the session. Called from the delete/terminate/
// gc control paths so a stopped-then-deleted session does not leak its entry
// in the per-Handler map for the gateway process lifetime.
func (f *turnStopFence) Delete(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.claimed, sessionID)
}
