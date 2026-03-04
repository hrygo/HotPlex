package base

import "sync"

// DangerApprovalRegistry manages pending danger block approvals.
// Used by chatapps to block on WAF interception until user approves/denies via Slack buttons.
type DangerApprovalRegistry struct {
	pending sync.Map // sessionID → chan bool
}

// GlobalDangerRegistry is the singleton registry for danger block approvals.
var GlobalDangerRegistry = &DangerApprovalRegistry{}

// Register creates a pending approval channel for the given sessionID.
// Returns the channel to block on. The caller should select on ctx.Done() and this channel.
func (r *DangerApprovalRegistry) Register(sessionID string) chan bool {
	ch := make(chan bool, 1)
	r.pending.Store(sessionID, ch)
	return ch
}

// Resolve resolves a pending approval for the given sessionID.
// Returns true if the sessionID was found and resolved, false otherwise.
func (r *DangerApprovalRegistry) Resolve(sessionID string, approved bool) bool {
	if val, ok := r.pending.LoadAndDelete(sessionID); ok {
		ch := val.(chan bool)
		ch <- approved
		return true
	}
	return false
}

// Cancel removes a pending approval without resolving it (e.g. on context cancellation).
func (r *DangerApprovalRegistry) Cancel(sessionID string) {
	r.pending.Delete(sessionID)
}
