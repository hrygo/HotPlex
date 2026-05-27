package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/tracing"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// platformWebChat is the platform tag for WebSocket webchat sessions.
const platformWebChat = "webchat"

// connHandler provides event handling capability for Conn.
type connHandler interface {
	Handle(ctx context.Context, env *events.Envelope) error
}

// connSM provides the session management subset that Conn needs for the
// resolveSession* series (init handshake, existing session resume, deleted
// session recreation) and ReadPump cleanup.
type connSM interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
	Transition(ctx context.Context, id string, to events.SessionState) error
	CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir, title string) (*session.SessionInfo, error)
	DeletePhysical(ctx context.Context, id string) error
}

// connAuth provides authentication capability for deferred-init auth.
type connAuth interface {
	AuthenticateKey(ctx context.Context, key string) (string, bool)
}

// SessionStarter initiates a worker session. It is the only Bridge capability
// used by Conn (called once during the AEP init handshake).
type SessionStarter interface {
	StartSession(ctx context.Context, id, userID, botID string,
		wt worker.WorkerType, allowedTools []string, workDir string, platform string, platformKey map[string]string, title string) error
	ResumeSession(ctx context.Context, id string, workDir string) error
	SwitchWorkDir(ctx context.Context, oldSessionID, newWorkDir string) (*SwitchWorkDirResult, error)
}

var _ SessionStarter = (*Bridge)(nil) // compile-time: Bridge implements SessionStarter

// Conn represents a single WebSocket client connection.
type Conn struct {
	log *slog.Logger
	wc  *websocket.Conn
	hub *Hub

	sessionID string
	userID    string
	botID     string // SEC-007: bot isolation tag from X-Bot-ID header or init envelope

	// pendingAuth defers authentication to the init envelope (browser WS clients).
	pendingAuth bool

	// starter handles session creation and worker lifecycle (nil = no-op, test mode).
	starter SessionStarter

	// Heartbeat.
	hb *heartbeat

	mu     sync.Mutex
	closed bool

	// Init-phase buffering: during the AEP init handshake, events from
	// Hub.routeMessage (state transitions during StartSession/ResumeSession)
	// are buffered here. This ensures init_ack is always the first
	// application-level message the client receives. Flushed by markInitDone.
	initDone    bool
	initPending [][]byte

	// writeCh decouples Hub.Run from WebSocket write latency. Hub.routeMessage
	// sends pre-encoded messages to writeCh (non-blocking); WritePump drains
	// and writes to the WebSocket. Prevents head-of-line blocking where one
	// slow client blocks all sessions for up to writeWait (10s).
	writeCh chan []byte

	done chan struct{}
}

// newConn creates a new Conn.
func newConn(hub *Hub, wc *websocket.Conn, sessionID string, starter SessionStarter) *Conn {
	log := slog.Default()
	if hub != nil {
		log = hub.log
	}
	c := &Conn{
		log:       log.With("component", "conn", "channel", "webchat"),
		wc:        wc,
		hub:       hub,
		starter:   starter,
		sessionID: sessionID,
		hb:        newHeartbeat(log),
		initDone:  true, // true by default; performInit sets false during handshake
		writeCh:   make(chan []byte, 64),
		done:      make(chan struct{}),
	}
	// Start the write pump immediately so WriteCtx/WriteMessage deliver data.
	// Conn.Close() (via done channel) handles cleanup.
	go c.WritePump()
	return c
}

// RemoteAddr returns the remote address of the client.
func (c *Conn) RemoteAddr() string {
	if c.wc != nil {
		return c.wc.RemoteAddr().String()
	}
	return "?"
}

// ReadPump pumps messages from the WebSocket to the hub's broadcast channel.
// It also handles pong responses, missed pong detection, and the AEP init handshake.
func (c *Conn) ReadPump(handler connHandler, sm connSM, auth connAuth) {
	defer func() {
		if r := recover(); r != nil {
			c.log.Error("gateway: panic in ReadPump", "session_id", c.sessionID, "panic", r, "stack", string(debug.Stack()))
		}
		c.markInitDone() // flush buffered events or release on init failure
		c.hb.Stop()

		// Transition to IDLE BEFORE unregistering so the state(idle) event
		// can be routed through Hub.Run while the conn is still in h.sessions.
		// If we unregister first, routeMessage finds no connections and the
		// state event is silently dropped.
		if c.sessionID != "" && sm != nil {
			if si, getErr := sm.Get(context.Background(), c.sessionID); getErr == nil && si != nil && si.State == events.StateRunning {
				if err := sm.Transition(context.Background(), c.sessionID, events.StateIdle); err != nil {
					c.log.Warn("gateway: conn close transition to idle", "session_id", c.sessionID, "err", err)
				}
			}
		}

		// Now safe to remove from routing — state event already queued.
		c.hub.UnregisterConn(c)

		_ = c.Close()
	}()

	c.wc.SetReadLimit(maxMessageSize)

	// Phase 1: AEP init handshake — read the first message.
	if err := c.performInit(auth, sm); err != nil {
		c.log.Warn("gateway: init handshake failed", "session_id", c.sessionID, "err", err)
		return
	}

	// Phase 2: Normal message loop.
	for {
		// Set read deadline for pong detection.
		_ = c.wc.SetReadDeadline(time.Now().Add(pongWait))

		// Pong handler: record that remote responded.
		c.wc.SetPongHandler(func(ping string) error {
			c.hb.MarkAlive()
			_ = c.wc.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		_, data, err := c.wc.ReadMessage()
		if err != nil {
			// Detect missed pong (read deadline exceeded).
			if isReadTimeout(err) {
				metrics.GatewayErrorsTotal.WithLabelValues("pong_timeout").Inc()
				if c.hb.MarkMissed() {
					c.log.Warn("gateway: max missed pongs, disconnecting",
						"session_id", c.sessionID)
					return
				}
			}
			if !errors.Is(err, websocket.ErrCloseSent) {
				c.log.Debug("gateway: read error", "session_id", c.sessionID, "err", err)
			}
			metrics.GatewayErrorsTotal.WithLabelValues("read_error").Inc()
			return
		}

		// Reset missed counter on any successful read.
		c.hb.MarkAlive()

		env, err := aep.DecodeLine(data)
		if err != nil {
			c.sendError(events.ErrCodeInvalidMessage, err.Error())
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
			continue
		}

		metrics.GatewayMessagesTotal.WithLabelValues("incoming", string(env.Event.Type)).Inc()

		// Stamp session ID, sequence number, and owner ID.
		env.SessionID = c.sessionID
		env.OwnerID = c.userID
		// P2: ping/pong are heartbeat control messages — don't consume seq.
		if env.Event.Type != events.Ping {
			env.Seq = c.hub.NextSeq(c.sessionID)
		}

		// Route to handler — skip tracing span for high-frequency pings.
		if env.Event.Type == events.Ping {
			if err := handler.Handle(context.Background(), env); err != nil {
				c.log.Debug("gateway: handle ping error", "err", err, "session_id", c.sessionID)
			}
			continue
		}

		_, span := tracing.SpanFromContext(context.Background()).Start(context.Background(), "conn.recv")
		span.SetAttributes(
			attribute.String("session_id", c.sessionID),
			attribute.String("event_type", string(env.Event.Type)),
			attribute.Int64("seq", env.Seq),
		)
		if err := handler.Handle(context.Background(), env); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.log.Debug("gateway: handle error", "err", err, "session_id", c.sessionID)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// performInit reads and processes the AEP init handshake message.
// It blocks until either an init message is processed or an error occurs.
func (c *Conn) performInit(auth connAuth, sm connSM) error {
	_, span := tracing.SpanFromContext(context.Background()).Start(context.Background(), "conn.init")
	defer span.End()
	start := time.Now()
	defer func() { metrics.InitHandshakeDuration.Observe(time.Since(start).Seconds()) }()

	env, initData, err := c.readAndValidateInit()
	if err != nil {
		return err
	}

	if err := c.authenticateInit(auth, initData); err != nil {
		return err
	}

	sessionID, si, err := c.resolveSession(env, initData, sm)
	if err != nil {
		return err
	}

	return c.finalizeInit(sessionID, si, initData, span)
}

// readAndValidateInit reads the first message, decodes it, and validates
// that it is a well-formed AEP init envelope.
func (c *Conn) readAndValidateInit() (*events.Envelope, InitData, error) {
	_ = c.wc.SetReadDeadline(time.Now().Add(30 * time.Second))

	_, data, err := c.wc.ReadMessage()
	if err != nil {
		return nil, InitData{}, fmt.Errorf("read init: %w", err)
	}

	env, err := aep.DecodeLine(data)
	if err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, "malformed message: "+err.Error())
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
		return nil, InitData{}, err
	}

	if env.Event.Type != events.Init {
		c.sendInitError(events.ErrCodeProtocolViolation, "expected init as first message, got "+string(env.Event.Type))
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeProtocolViolation)).Inc()
		return nil, InitData{}, fmt.Errorf("expected init, got %s", env.Event.Type)
	}

	metrics.GatewayMessagesTotal.WithLabelValues("incoming", string(events.Init)).Inc()

	initData, initErr := ValidateInit(env)
	if initErr != nil {
		c.sendInitError(initErr.Code, initErr.Message)
		metrics.GatewayErrorsTotal.WithLabelValues(string(initErr.Code)).Inc()
		return nil, InitData{}, initErr
	}

	return env, initData, nil
}

// authenticateInit handles deferred authentication for browser WS clients
// that cannot send custom HTTP headers.
func (c *Conn) authenticateInit(auth connAuth, initData InitData) error {
	if c.pendingAuth {
		if initData.Auth.Token == "" {
			c.sendInitError(events.ErrCodeUnauthorized, "authentication required")
			return fmt.Errorf("deferred auth: no token in init envelope")
		}
		uid, ok := auth.AuthenticateKey(context.Background(), initData.Auth.Token)
		if !ok {
			c.sendInitError(events.ErrCodeUnauthorized, "invalid token")
			return fmt.Errorf("deferred auth: invalid token")
		}
		c.userID = uid
		c.pendingAuth = false
	}

	if c.botID == "" && initData.Auth.BotID != "" {
		c.botID = initData.Auth.BotID
	}
	return nil
}

// resolveSession resolves the session ID, checks throttling, and ensures
// the session exists and is in the correct state (create/resume/fast-reconnect).
func (c *Conn) resolveSession(env *events.Envelope, initData InitData, sm connSM) (string, *session.SessionInfo, error) {
	workDir := initData.Config.WorkDir
	if workDir == "" {
		workDir = c.hub.cfgStore.Load().Worker.DefaultWorkDir
	}
	expanded, err := validateAndExpandWorkDir(workDir)
	if err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, err.Error())
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
		return "", nil, err
	}
	workDir = expanded

	var sessionID string
	var preResolved *session.SessionInfo
	if env.SessionID != "" {
		if existing, getErr := sm.Get(context.Background(), env.SessionID); getErr == nil && existing != nil && existing.State != events.StateDeleted {
			sessionID = env.SessionID
			preResolved = existing
		}
	}
	if sessionID == "" {
		sessionID = session.DeriveSessionKey(c.userID, initData.WorkerType, env.SessionID, workDir)
	}

	if !c.hub.InitThrottle.Check(sessionID) {
		c.sendInitError(events.ErrCodeRateLimited, "too many failed attempts, please back off")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeRateLimited)).Inc()
		return "", nil, fmt.Errorf("init throttled for session %s", sessionID)
	}

	c.mu.Lock()
	c.initDone = false
	c.mu.Unlock()

	c.hub.LeaveSession("", c)
	c.hub.JoinSession(sessionID, c)

	return c.resolveSessionState(sessionID, initData, workDir, sm, preResolved)
}

// resolveSessionState handles the session state machine transitions:
// not-found → create, created → start, deleted → recreate,
// idle/terminated → resume (with fresh-start fallback), running+alive → fast reconnect.
func (c *Conn) resolveSessionState(sessionID string, initData InitData, workDir string, sm connSM, preResolved *session.SessionInfo) (string, *session.SessionInfo, error) {
	var si *session.SessionInfo
	var err error

	if preResolved != nil {
		si = preResolved
	} else {
		si, err = sm.Get(context.Background(), sessionID)
	}

	if err != nil {
		result, handleErr := c.handleSessionNotFound(sessionID, initData, workDir, sm, err)
		return sessionID, result, handleErr
	}

	switch si.State {
	case events.StateCreated:
		result, stateErr := c.startCreatedSession(sessionID, initData, workDir, sm, si)
		return sessionID, result, stateErr
	case events.StateDeleted:
		result, stateErr := c.recreateDeletedSession(sessionID, initData, workDir, sm)
		return sessionID, result, stateErr
	default:
		result, stateErr := c.handleExistingSession(sessionID, workDir, sm, si, initData)
		return sessionID, result, stateErr
	}
}

func (c *Conn) handleSessionNotFound(sessionID string, initData InitData, workDir string, sm connSM, lookupErr error) (*session.SessionInfo, error) {
	if !errors.Is(lookupErr, session.ErrSessionNotFound) {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, lookupErr.Error())
		return nil, fmt.Errorf("get session: %w", lookupErr)
	}

	if c.starter != nil {
		if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, "failed to create session")
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
			return nil, fmt.Errorf("create session: %w", err)
		}
		si, err := sm.Get(context.Background(), sessionID)
		if err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, "session not found after creation")
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
			return nil, fmt.Errorf("get session after start: %w", err)
		}
		return si, nil
	}

	// Test mode: create directly via session manager.
	si, err := sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, platformWebChat, nil, workDir, "")
	if err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, "failed to create session")
		return nil, fmt.Errorf("create session: %w", err)
	}
	return si, nil
}

func (c *Conn) startCreatedSession(sessionID string, initData InitData, workDir string, sm connSM, si *session.SessionInfo) (*session.SessionInfo, error) {
	if c.starter == nil {
		return si, nil // no starter in test mode, session stays CREATED
	}
	if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, "failed to start session")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
		return nil, fmt.Errorf("start unstarted session: %w", err)
	}
	si, err := sm.Get(context.Background(), sessionID)
	if err != nil {
		c.sendInitError(events.ErrCodeInternalError, "session lost after creation")
		return nil, fmt.Errorf("get session after start: %w", err)
	}
	return si, nil
}

func (c *Conn) recreateDeletedSession(sessionID string, initData InitData, workDir string, sm connSM) (*session.SessionInfo, error) {
	_ = sm.DeletePhysical(context.Background(), sessionID)
	if c.starter == nil {
		// Test mode: re-create session directly since the old one was physically deleted.
		newSI, err := sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, platformWebChat, nil, workDir, "")
		if err != nil {
			return nil, fmt.Errorf("recreate deleted session (test mode): %w", err)
		}
		return newSI, nil
	}
	if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID,
		initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, fmt.Sprintf("failed to recreate deleted session: %v", err))
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
		return nil, fmt.Errorf("recreate deleted session: %w", err)
	}
	si, err := sm.Get(context.Background(), sessionID)
	if err != nil {
		c.sendInitError(events.ErrCodeInternalError, "session lost after recreation")
		return nil, fmt.Errorf("get session after recreation: %w", err)
	}
	return si, nil
}

func (c *Conn) handleExistingSession(sessionID, workDir string, sm connSM, si *session.SessionInfo, initData InitData) (*session.SessionInfo, error) {
	// Fast reconnect: worker still alive, skip terminate+resume cycle.
	if w := sm.GetWorker(sessionID); w != nil {
		if si.State != events.StateRunning {
			if err := sm.Transition(context.Background(), sessionID, events.StateRunning); err != nil {
				c.log.Warn("gateway: fast reconnect transition failed", "session_id", sessionID, "from", si.State, "err", err)
			} else {
				si.State = events.StateRunning
			}
		}
		return si, nil
	}

	// State guard: Created/Deleted are handled by startCreatedSession/recreateDeletedSession
	// in resolveSessionState; only idle, terminated, or running (zombie) reach this resume path.
	if si.State != events.StateIdle && si.State != events.StateTerminated && si.State != events.StateRunning {
		return si, nil
	}

	if c.starter == nil {
		return si, nil
	}

	resumeErr := c.starter.ResumeSession(context.Background(), sessionID, workDir)
	if resumeErr != nil {
		if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID,
			initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			msg := fmt.Sprintf("resume failed (%v), then start also failed (%v)", resumeErr, err)
			c.sendInitError(events.ErrCodeInternalError, msg)
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
			return nil, fmt.Errorf("start session after resume fallback: %w", err)
		}
	}
	si, err := sm.Get(context.Background(), sessionID)
	if err != nil {
		c.sendInitError(events.ErrCodeInternalError, "session lost after resume")
		return nil, fmt.Errorf("get session after resume: %w", err)
	}
	return si, nil
}

// finalizeInit performs security checks, sends init_ack, and marks init complete.
func (c *Conn) finalizeInit(sessionID string, si *session.SessionInfo, initData InitData, span trace.Span) error {
	// SEC-008: reject cross-user access on reconnect.
	if c.userID != "" && si.UserID != "" && c.userID != si.UserID {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeUnauthorized, "user_id mismatch")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeUnauthorized)).Inc()
		return fmt.Errorf("user_id mismatch: connection=%s session=%s", c.userID, si.UserID)
	}

	// SEC-007: reject cross-bot access.
	if c.botID != "" && si.BotID != "" && c.botID != si.BotID {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeUnauthorized, "bot_id mismatch")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeUnauthorized)).Inc()
		return fmt.Errorf("bot_id mismatch: connection=%s session=%s", c.botID, si.BotID)
	}

	c.hub.InitThrottle.RecordSuccess(sessionID)

	c.mu.Lock()
	c.sessionID = sessionID
	c.userID = si.UserID
	c.mu.Unlock()

	ack := BuildInitAck(sessionID, si.State, initData.WorkerType)
	ack.Seq = c.hub.NextSeq(sessionID)
	if err := c.WriteCtx(context.Background(), ack); err != nil {
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
		return fmt.Errorf("send init_ack: %w", err)
	}
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", InitAck).Inc()

	c.markInitDone()

	c.log.Info("gateway: init complete", "session_id", sessionID,
		"worker_type", initData.WorkerType, "state", si.State)
	span.SetStatus(codes.Ok, "init complete")
	return nil
}

// writeSync writes data directly to the WebSocket under lock.
// Used in cleanup paths (init errors, close) where async delivery via writeCh
// would race with the subsequent Close() call.
func (c *Conn) writeSync(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("conn closed")
	}
	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	return c.wc.WriteMessage(websocket.TextMessage, data)
}

func (c *Conn) sendInitError(code events.ErrorCode, msg string) {
	ack := BuildInitAckError(c.sessionID, &InitError{Code: code, Message: msg})
	ack.Seq = c.hub.NextSeq(c.sessionID)
	data, err := aep.EncodeJSON(ack)
	if err != nil {
		return
	}
	_ = c.writeSync(data)
}

// WritePump pumps periodic pings to the WebSocket.
// It also drains the hub's broadcast channel and writes to the client.
func (c *Conn) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer func() {
		if r := recover(); r != nil {
			c.log.Error("gateway: panic in WritePump", "session_id", c.sessionID, "panic", r, "stack", string(debug.Stack()))
			_ = c.Close()
		}
	}()

	for {
		select {
		case <-c.done:
			return
		case <-c.hb.Stopped():
			return
		case data, ok := <-c.writeCh:
			if !ok {
				return
			}
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.wc.WriteMessage(websocket.TextMessage, data); err != nil {
				c.mu.Unlock()
				c.log.Debug("gateway: write failed", "session_id", c.sessionID, "err", err)
				return
			}
			c.mu.Unlock()
		case <-ticker.C:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.wc.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.mu.Unlock()
				c.log.Debug("gateway: ping failed", "session_id", c.sessionID, "err", err)
				return
			}
			c.mu.Unlock()
		}
	}
}

// WriteCtx writes an envelope to the connection using the provided context for deadline.
func (c *Conn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	data, err := aep.EncodeJSON(env)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("conn closed")
	}
	c.mu.Unlock()

	select {
	case c.writeCh <- data:
		return nil
	case <-c.done:
		return errors.New("conn closed")
	}
}

// RouteWrite writes an envelope through the Hub routing path. It handles
// init-phase buffering and applies droppable semantics for delta/raw events
// (silently drops on full channel instead of disconnecting).
func (c *Conn) RouteWrite(_ context.Context, env *events.Envelope) error {
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(env.Event.Type)).Inc()
	data, err := aep.EncodeJSON(env)
	if err != nil {
		return err
	}
	// Handle init-phase buffering and closed check.
	if handled, err := c.bufferOrReject(data); handled {
		return err
	}
	// Post-init: apply droppable vs reliable write semantics.
	if isDroppable(env.Event.Type) {
		return c.trySendData(data)
	}
	return c.sendData(data)
}

// sendData writes pre-encoded data to the write channel. Disconnects the
// client if the channel is full (backpressure for reliable events).
func (c *Conn) sendData(data []byte) error {
	select {
	case c.writeCh <- data:
		return nil
	default:
		c.log.Warn("gateway: slow client, write channel full, disconnecting", "session_id", c.sessionID)
		metrics.GatewayErrorsTotal.WithLabelValues("slow_client").Inc()
		_ = c.Close()
		return errors.New("write channel full, slow client disconnected")
	}
}

// trySendData attempts to write pre-encoded data without blocking.
// Silently drops the message if the channel is full (for droppable events).
func (c *Conn) trySendData(data []byte) error {
	select {
	case c.writeCh <- data:
		return nil
	default:
		metrics.GatewayDeltasDropped.Inc()
		return nil
	}
}

// WriteMessage writes raw bytes to the connection.
// During the AEP init handshake, events are buffered instead of written
// to ensure init_ack is always the first message the client receives.
// After init, sends to writeCh for WritePump to drain (non-blocking).
// If the write channel is full, the client is disconnected to protect Hub.Run.
func (c *Conn) WriteMessage(msgType int, data []byte) error {
	if handled, err := c.bufferOrReject(data); handled {
		return err
	}

	select {
	case c.writeCh <- data:
		return nil
	default:
		// Slow client — write channel full. Disconnect to protect Hub.Run.
		c.log.Warn("gateway: slow client, write channel full, disconnecting", "session_id", c.sessionID)
		metrics.GatewayErrorsTotal.WithLabelValues("slow_client").Inc()
		_ = c.Close()
		return errors.New("write channel full, slow client disconnected")
	}
}

// TryWriteMessage attempts to write raw bytes to the connection without blocking.
// Unlike WriteMessage, it silently drops the message if the write channel is full
// instead of disconnecting the client. Use for droppable events (message.delta, raw).
func (c *Conn) TryWriteMessage(msgType int, data []byte) error {
	if handled, err := c.bufferOrReject(data); handled {
		return err
	}

	select {
	case c.writeCh <- data:
		return nil
	default:
		return nil // silently dropped
	}
}

// bufferOrReject handles the closed-check and init-phase buffering shared by
// WriteMessage and TryWriteMessage. Returns (true, err) if the message was
// handled (buffered or rejected), or (false, nil) if the caller should proceed
// to send to writeCh.
func (c *Conn) bufferOrReject(data []byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return true, errors.New("conn closed")
	}
	if !c.initDone {
		buf := make([]byte, len(data))
		copy(buf, data)
		c.initPending = append(c.initPending, buf)
		return true, nil
	}
	return false, nil
}

// markInitDone signals that the init handshake is complete and flushes
// any events buffered by WriteMessage during init. After this call,
// WriteMessage sends to writeCh for WritePump to drain.
func (c *Conn) markInitDone() {
	c.mu.Lock()
	c.initDone = true
flushLoop:
	for _, data := range c.initPending {
		if c.closed {
			break
		}
		select {
		case c.writeCh <- data:
		default:
			c.log.Warn("gateway: init flush write channel full", "session_id", c.sessionID)
			_ = c.Close()
			break flushLoop
		}
	}
	c.initPending = nil
	c.mu.Unlock()
}

// Close closes the WebSocket connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	_ = c.wc.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.mu.Unlock()

	return c.wc.Close()
}

func (c *Conn) sendError(code events.ErrorCode, msg string) {
	env := events.NewEnvelope(aep.NewID(), c.sessionID, c.hub.NextSeq(c.sessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: msg,
	})
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(events.Error)).Inc()
	_ = c.WriteCtx(context.Background(), env)
}
