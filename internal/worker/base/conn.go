package base

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Conn implements worker.SessionConn for stdin-based workers (claudecode, opencodeserver).
type Conn struct {
	userID    string
	sessionID string
	stdin     *os.File
	recvCh    chan *events.Envelope
	log       *slog.Logger
	mu        sync.Mutex
	closed    bool
	lastInput string // last user message content; used for crash recovery re-delivery
}

// NewConn creates a new stdin-based session connection.
func NewConn(log *slog.Logger, stdin *os.File, userID, sessionID string) *Conn {
	if log == nil {
		log = slog.Default()
	}
	return &Conn{
		userID:    userID,
		sessionID: sessionID,
		stdin:     stdin,
		recvCh:    make(chan *events.Envelope, 256),
		log:       log,
	}
}

// Send delivers a message to the worker runtime via stdin using NDJSON encoding.
func (c *Conn) Send(ctx context.Context, msg *events.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return &worker.WorkerError{Kind: worker.ErrKindUnavailable, Message: "base: connection closed"}
	}

	// Write NDJSON to stdin while holding the lock to prevent interleaving
	// with ControlHandler writes on the same fd.
	if err := aep.Encode(c.stdin, msg); err != nil {
		if IsDeadProcessError(err) {
			return &worker.WorkerError{Kind: worker.ErrKindUnavailable, Message: "base: worker process is not running or stdin is closed", Cause: err}
		}
		return fmt.Errorf("base: encode: %w", err)
	}

	return nil
}

// Recv returns a channel that yields messages from the worker runtime.
func (c *Conn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

func (c *Conn) TrySend(env *events.Envelope) bool {
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}

// WriteMu returns the mutex that protects stdin writes.
// ControlHandler should use this same mutex to serialize stdin access.
func (c *Conn) WriteMu() *sync.Mutex {
	return &c.mu
}

// Stdin returns the underlying stdin file.
//
// Deprecated: Use StdinLocked() instead. The returned *os.File is unprotected
// after the internal mutex is released, making it unsafe for concurrent writes.
func (c *Conn) Stdin() *os.File {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdin
}

// StdinLocked returns the stdin file and the protecting mutex (locked).
// Caller must unlock the returned mutex after completing all operations.
// Use for atomic stdin-read + write + SetLastInput sequences.
func (c *Conn) StdinLocked() (*os.File, *sync.Mutex) {
	c.mu.Lock()
	return c.stdin, &c.mu
}

// SetLastInput records the content of the most recent user message.
// Worker adapters should call this when they deliver user input through
// protocol-specific channels, so the bridge crash recovery mechanism
// can re-deliver the message after a resume failure.
func (c *Conn) SetLastInput(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastInput = content
}

// SetLastInputLocked sets lastInput. Caller must hold c.mu.
func (c *Conn) SetLastInputLocked(content string) {
	c.lastInput = content
}

// CloseInput closes the stdin pipe to signal EOF to the worker process.
// The worker will finish processing buffered input and exit.
// Safe to call multiple times; sets stdin to nil after close.
func (c *Conn) CloseInput() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		err := c.stdin.Close()
		c.stdin = nil
		return err
	}
	return nil
}

// Close terminates the connection and releases resources.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	close(c.recvCh)

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	return nil
}

// UserID returns the user who owns this session.
func (c *Conn) UserID() string {
	return c.userID
}

// SessionID returns the session identifier.
func (c *Conn) SessionID() string {
	return c.sessionID
}

// SetSessionID updates the session identifier (for opencodeserver's session ID extraction).
func (c *Conn) SetSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}

// LastInput returns the content of the most recent user message.
// Used by bridge crash recovery to re-deliver input to a fresh worker after resume failure.
func (c *Conn) LastInput() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastInput
}
