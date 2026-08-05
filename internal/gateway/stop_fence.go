package gateway

import "sync"

// turnStopFence is the per-turn single-stop admission fence for control.stop
// (Task 5: Gateway stop/next-turn 单终态合同).
//
// Contract: at most ONE effective StopCurrentTurn per (session, worker run)
// per turn. The stop path Claims BEFORE the Worker call; a second stop for the
// same session/run is admitted=false and returns immediately — no Worker call,
// no second Done, no second runtime finish. handleInput clears the claim
// (BeginTurn) before each new primary turn, so a later turn can stop again
// even when the same worker run spans turns. StopCurrentTurn failures release
// the claim (Rollback) so a manual retry can stop again; a successful stop
// keeps the claim for the turn.
//
// Zero value is ready to use: the claimed map is lazily initialized under the
// lock, so handlers constructed without NewHandler (unit tests) stay safe.
//
// Explicit mu field per project convention (no embedding, no pointer passing).
type turnStopFence struct {
	mu      sync.Mutex
	claimed map[string]string // session ID -> worker run ID
}

// Claim admits the stop for sessionID/workerRunID. It returns true only for
// the first caller; a duplicate claim for the same session/run returns false
// (the gateway must not call StopCurrentTurn again). A claim for the same
// session with a DIFFERENT run (worker replaced) is admitted and overwrites
// the stale entry.
func (f *turnStopFence) Claim(sessionID, workerRunID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed == nil {
		f.claimed = make(map[string]string)
	}
	if prev, ok := f.claimed[sessionID]; ok && prev == workerRunID {
		return false
	}
	f.claimed[sessionID] = workerRunID
	return true
}

// Rollback releases the claim after a FAILED StopCurrentTurn so a manual retry
// can stop again. It only clears when the claim still belongs to the given
// run — an old run's rollback must not clear a newer run's claim.
func (f *turnStopFence) Rollback(sessionID, workerRunID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prev, ok := f.claimed[sessionID]; ok && prev == workerRunID {
		delete(f.claimed, sessionID)
	}
}

// BeginTurn clears the previous turn's stop claim for the session before a new
// primary turn (handleInput, after worker binding / MarkRunning, before the
// new primary w.Input). Scoped to the same session and the same worker run:
// a different session or a replaced worker run is untouched.
func (f *turnStopFence) BeginTurn(sessionID, workerRunID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prev, ok := f.claimed[sessionID]; ok && prev == workerRunID {
		delete(f.claimed, sessionID)
	}
}
