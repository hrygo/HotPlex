package acp

import (
	"context"
	"sync"

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

// TrySend enqueues an envelope from readLoop (non-blocking, backpressure-aware).
func (c *acpConn) TrySend(env *events.Envelope) bool {
	select {
	case c.recvCh <- env:
		return true
	default:
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
