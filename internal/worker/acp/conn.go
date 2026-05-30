package acp

import (
	"context"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// acpConn implements worker.SessionConn for ACP workers.
// Unlike base.Conn (which wraps stdin for AEP NDJSON), acpConn only provides
// the "up" direction (readLoop → TrySend → Recv → forwardEvents).
// User input and permission responses go through client.Prompt / client.RespondPermission.
type acpConn struct {
	userID    string
	sessionID string
	recvCh    chan *events.Envelope
	mu        sync.Mutex
	closed    bool
}

// Compile-time check.
var _ worker.SessionConn = (*acpConn)(nil)

func newACPConn(userID, sessionID string) *acpConn {
	return &acpConn{
		userID:    userID,
		sessionID: sessionID,
		recvCh:    make(chan *events.Envelope, 256),
	}
}

// Send is not used by ACP workers — user input goes through Worker.Input → client.Prompt.
func (c *acpConn) Send(_ context.Context, msg *events.Envelope) error {
	return worker.ErrNotImplemented
}

// Recv returns the channel that forwardEvents ranges over.
func (c *acpConn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

// TrySend enqueues an envelope from readLoop (backpressure-aware).
// Critical events (state/done/error/permission_request/question_request/elicitation_request)
// block until sent; droppable events (message.delta/raw) are silently discarded when full.
// All channel sends are protected by recover() to prevent send-on-closed-channel panics
// during the shutdown race between TrySend and Close.
func (c *acpConn) TrySend(env *events.Envelope) bool {
	if isDroppable(env.Event.Type) {
		return c.trySendNonBlocking(env)
	}
	// Critical event: try non-blocking first, then blocking with closed-channel check.
	if c.trySendNonBlocking(env) {
		return true
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}
	return c.safeSend(env)
}

// isDroppable reports whether an event type can be silently discarded under backpressure.
func isDroppable(kind events.Kind) bool {
	return kind == events.MessageDelta || kind == events.Raw
}

// trySendNonBlocking attempts a non-blocking send with panic recovery.
// Returns false if the channel is full, closed, or a panic was recovered.
func (c *acpConn) trySendNonBlocking(env *events.Envelope) (sent bool) {
	defer func() { _ = recover() }()
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}

// safeSend performs a blocking send on recvCh with panic recovery and timeout.
// This protects against the TOCTOU race where Close() shuts down recvCh
// between the closed-flag check and the actual send.
// A 5s timeout prevents readLoop deadlock when forwardEvents is slow.
func (c *acpConn) safeSend(env *events.Envelope) (sent bool) {
	defer func() { _ = recover() }()
	select {
	case c.recvCh <- env:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// Close shuts down the receive channel.
func (c *acpConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.recvCh)
	return nil
}

func (c *acpConn) UserID() string    { return c.userID }
func (c *acpConn) SessionID() string { return c.sessionID }
