package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// PermissionDenyDedup suppresses repeated permission requests for the same
// owner+fingerprint within a rolling window after a user denial. It closes the
// "deny → agent retries the same tool seconds later → brand-new card" loop:
// every layer previously treated each PermissionRequest as independent because
// denials were per-request-id and uncached.
//
// Session-scoped (bySession) and owner-scoped (cacheKey prefixes ownerID so
// group chats don't cross-contaminate). Nil-safe: a nil receiver is a no-op,
// so the feature is gated by leaving Bridge.dedup nil when disabled.
type PermissionDenyDedup struct {
	window    time.Duration
	now       func() time.Time
	mu        sync.Mutex
	bySession map[string]*dedupState
}

type dedupState struct {
	denied   map[string]time.Time // cacheKey(ownerID|fp) → expireAt
	reqIndex map[string]string    // reqID → fp (awaiting user response)
}

func newPermissionDenyDedup(window time.Duration, now func() time.Time) *PermissionDenyDedup {
	if now == nil {
		now = time.Now
	}
	return &PermissionDenyDedup{
		window:    window,
		now:       now,
		bySession: make(map[string]*dedupState),
	}
}

// RegisterRequest records an outgoing PermissionRequest and reports whether the
// same owner+fingerprint was denied within the window. A hit means the caller
// should suppress the card and deliver a local denial to the worker instead of
// forwarding to the client. On miss, the reqID→fp mapping is registered so a
// subsequent RecordDeny can resolve it back. Hit path does not touch reqIndex:
// the suppressed local denial never re-enters via RecordDeny, so its reqID has
// nothing to resolve and must not leak an entry.
func (d *PermissionDenyDedup) RegisterRequest(sessionID, reqID, ownerID, fp string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	st := d.stateFor(sessionID)
	key := cacheKey(ownerID, fp)
	if exp, ok := st.denied[key]; ok {
		if exp.After(d.now()) {
			return true
		}
		delete(st.denied, key) // expired — lazy evict
	}
	st.reqIndex[reqID] = fp
	return false
}

// RecordDeny resolves a reqID to its fingerprint (registered at outbound time)
// and stamps the denied cache for ownerID+fp with a fresh window. No-op if the
// reqID is unknown (suppressed local denial, foreign session, or already
// resolved). Called on the deny inflow path; the allow path never invokes it.
func (d *PermissionDenyDedup) RecordDeny(sessionID, reqID, ownerID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	st, ok := d.bySession[sessionID]
	if !ok {
		return
	}
	fp, ok := st.reqIndex[reqID]
	if !ok {
		return
	}
	st.denied[cacheKey(ownerID, fp)] = d.now().Add(d.window)
	delete(st.reqIndex, reqID)
}

// ClearSession drops all dedup state for a session. Called on session end, GC,
// and /reset to prevent unbounded growth and cross-turn leakage.
func (d *PermissionDenyDedup) ClearSession(sessionID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.bySession, sessionID)
}

func (d *PermissionDenyDedup) stateFor(sessionID string) *dedupState {
	st := d.bySession[sessionID]
	if st == nil {
		st = &dedupState{
			denied:   make(map[string]time.Time),
			reqIndex: make(map[string]string),
		}
		d.bySession[sessionID] = st
	}
	return st
}

func cacheKey(ownerID, fp string) string {
	return ownerID + "\x00" + fp
}

// ComputeFingerprint produces a stable hash for a tool invocation. Tool name is
// joined with a canonicalized form of args: if args[0] parses as JSON it is
// re-serialized with sorted object keys (so reordered keys don't evade the
// cache); otherwise the raw args are joined with a unit separator. Returns a
// 16-byte truncated SHA-256, hex-encoded.
func ComputeFingerprint(toolName string, args []string) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte{0})
	h.Write([]byte(canonicalArgs(args)))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func canonicalArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		if norm, ok := normalizeJSON(args[0]); ok {
			return norm
		}
	}
	return strings.Join(args, "\x1f")
}

// normalizeJSON re-serializes a JSON string with sorted map keys. encoding/json
// guarantees object keys are sorted on Marshal, so the round-trip yields a
// canonical form regardless of the input's original key order. Returns false if
// the input is not valid JSON.
func normalizeJSON(s string) (string, bool) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(b), true
}
