package opencodeserver

import (
	"encoding/json"
	"sync"
	"time"
)

// OpenCode's built-in retry stays in retry state until status.next and then
// resumes directly. A retry followed by idle is instead an abort/fallback
// handoff (for example, a plugin switching models), whose next busy event is
// expected promptly. Do not extend this grace to status.next: quota errors can
// put that timestamp days in the future.
const retryTerminalGracePeriod = time.Second

type retryTimer interface {
	Stop() bool
}

type retryAfterFunc func(time.Duration, func()) retryTimer

type pendingRetryTerminal struct {
	message string
	timer   retryTimer
}

// retryTerminalArbiter distinguishes a transient idle between OpenCode retry
// attempts from the idle that actually terminates a failed turn.
type retryTerminalArbiter struct {
	mu        sync.Mutex
	afterFunc retryAfterFunc
	pending   map[string]*pendingRetryTerminal
}

func newRetryTerminalArbiter(afterFunc retryAfterFunc) *retryTerminalArbiter {
	return &retryTerminalArbiter{
		afterFunc: afterFunc,
		pending:   make(map[string]*pendingRetryTerminal),
	}
}

// deferEvent records retry metadata and reports whether an idle event must be
// held during the grace period. Any continuation event cancels the pending
// terminal because OpenCode has started its fallback attempt.
func (a *retryTerminalArbiter) deferEvent(
	sessionID, eventType string,
	props json.RawMessage,
	onExpired func(*pendingRetryTerminal),
) bool {
	statusType, message, hasStatus := parseRetryStatus(eventType, props)

	a.mu.Lock()
	defer a.mu.Unlock()

	if hasStatus && statusType == "retry" {
		pending := a.pending[sessionID]
		if pending == nil {
			pending = &pendingRetryTerminal{}
			a.pending[sessionID] = pending
		}
		if pending.timer != nil {
			pending.timer.Stop()
			pending.timer = nil
		}
		if message != "" {
			pending.message = message
		}
		return false
	}
	if eventType == ocsSessionStatus && !hasStatus {
		return false
	}

	isIdle := eventType == ocsSessionIdle || hasStatus && statusType == "idle"
	pending := a.pending[sessionID]
	if isIdle && pending != nil {
		if pending.timer == nil {
			token := pending
			pending.timer = a.afterFunc(retryTerminalGracePeriod, func() { onExpired(token) })
		}
		return true
	}

	if pending != nil {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(a.pending, sessionID)
	}
	return false
}

func (a *retryTerminalArbiter) consumeExpired(
	sessionID string,
	token *pendingRetryTerminal,
) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	pending := a.pending[sessionID]
	if pending == nil || pending != token || pending.timer == nil {
		return "", false
	}
	delete(a.pending, sessionID)
	return pending.message, true
}

func (a *retryTerminalArbiter) cancel(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if pending := a.pending[sessionID]; pending != nil {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(a.pending, sessionID)
	}
}

func (a *retryTerminalArbiter) cancelAll() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for sessionID, pending := range a.pending {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(a.pending, sessionID)
	}
}

func parseRetryStatus(eventType string, props json.RawMessage) (statusType, message string, ok bool) {
	if eventType != ocsSessionStatus {
		return "", "", false
	}
	var data struct {
		Status struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"status"`
	}
	if err := json.Unmarshal(props, &data); err != nil || data.Status.Type == "" {
		return "", "", false
	}
	return data.Status.Type, data.Status.Message, true
}
