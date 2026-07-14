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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/session"
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
	CreateWithBot(ctx context.Context, id, userID, botID, botName string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir, title, clientKey string) (*session.SessionInfo, error)
	DeletePhysical(ctx context.Context, id string) error
}

// connAuth provides authentication capability for deferred-init auth.
type connAuth interface {
	AuthenticateKey(ctx context.Context, key string) (string, bool)
}

// SessionStarter initiates a worker session. It is the only Bridge capability
// used by Conn (called once during the AEP init handshake).
type SessionStarter interface {
	StartSession(ctx context.Context, p worker.SessionStartParams) error
	ResumeSession(ctx context.Context, id string, workDir string) error
	SwitchWorkDir(ctx context.Context, oldSessionID, newWorkDir string) (*SwitchWorkDirResult, error)
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
}

var _ SessionStarter = (*Bridge)(nil) // compile-time: Bridge implements SessionStarter

// Conn represents a single WebSocket client connection.
type Conn struct {
	log *slog.Logger
	wc  *websocket.Conn
	hub *Hub

	sessionID   string
	userID      string
	botID       string // SEC-007: bot isolation tag from X-Bot-ID header or init envelope
	workspaceID string // workspace ID

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
	//
	// Buffer size 256: deltas are committed synchronously (no RAF batching —
	// browser-throttled APIs in background tabs caused unbounded accumulation
	// and tail loss). Synchronous commits mean every delta hits writeCh
	// individually; the larger buffer absorbs per-delta bursts that the
	// previous ≤60fps frame coalescer smoothed. Do not reduce without
	// re-introducing client-side batching that is safe in background tabs.
	writeCh chan []byte

	done chan struct{}

	// auditCollector is the optional audit log collector (issue #833).
	// When non-nil, authentication and message events are recorded.
	auditCollector *audit.Collector
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
		writeCh:   make(chan []byte, 256),
		done:      make(chan struct{}),
	}
	// Start the write pump immediately so WriteCtx/WriteMessage deliver data.
	// Conn.Close() (via done channel) handles cleanup.
	go c.WritePump()
	return c
}

// SetAuditCollector injects the audit collector for authentication event recording.
func (c *Conn) SetAuditCollector(ac *audit.Collector) {
	c.auditCollector = ac
}

// emitAudit enqueues a non-blocking audit event. No-op when collector is nil.
func (c *Conn) emitAudit(action, outcome, userID, platform, sessionID, ip string) {
	if c.auditCollector == nil {
		return
	}
	if userID == "" {
		userID = audit.AnonymousUserID
	}
	_ = c.auditCollector.Enqueue(context.Background(), &audit.UserActivity{
		Ts:         time.Now().UnixMilli(),
		UserID:     userID,
		UserIDType: audit.UserIDTypePlatform,
		Platform:   platform,
		SessionID:  sessionID,
		Action:     action,
		Outcome:    outcome,
		DetailJSON: `{}`,
		IP:         ip,
	})
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
				observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", "pong_timeout")))
				if c.hb.MarkMissed() {
					c.log.Warn("gateway: max missed pongs, disconnecting",
						"session_id", c.sessionID)
					return
				}
			}
			if !errors.Is(err, websocket.ErrCloseSent) {
				c.log.Debug("gateway: read error", "session_id", c.sessionID, "err", err)
			}
			observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", "read_error")))
			return
		}

		// Reset missed counter on any successful read.
		c.hb.MarkAlive()

		env, err := aep.DecodeLineMinimal(data)
		if err != nil {
			c.sendError(events.ErrCodeInvalidMessage, err.Error())
			observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
			continue
		}

		observability.GatewayMessages().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("direction", "incoming"), attribute.String("event_type", string(env.Event.Type))))

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

		_, span := observability.Tracer().Start(context.Background(), "conn.recv")
		span.SetAttributes(
			attribute.String("session_id", c.sessionID),
			attribute.String("event_type", string(env.Event.Type)),
			attribute.Int64("seq", env.Seq),
		)

		isInteraction := env.Event.Type == events.PermissionResponse ||
			env.Event.Type == events.QuestionResponse ||
			env.Event.Type == events.ElicitationResponse

		if env.Event.Type == events.Input {
			if dataMap, ok := env.Event.Data.(map[string]any); ok {
				if md, ok := dataMap["metadata"].(map[string]any); ok {
					if md["permission_response"] != nil ||
						md["question_response"] != nil ||
						md["elicitation_response"] != nil {
						isInteraction = true
					}
				}
			}
		}

		if isInteraction {
			go func(e *events.Envelope, s trace.Span) {
				defer s.End()
				if err := handler.Handle(context.Background(), e); err != nil {
					s.RecordError(err)
					s.SetStatus(codes.Error, err.Error())
					c.log.Warn("gateway: handle interaction response async error", "err", err, "session_id", c.sessionID)
				} else {
					s.SetStatus(codes.Ok, "")
				}
			}(env, span)
		} else {
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
}

// performInit reads and processes the AEP init handshake message.
// It blocks until either an init message is processed or an error occurs.
func (c *Conn) performInit(auth connAuth, sm connSM) error {
	_, span := observability.Tracer().Start(context.Background(), "conn.init")
	defer span.End()
	start := time.Now()
	defer func() {
		observability.GatewayInitHandshakeDuration().Record(c.hub.ctx, time.Since(start).Seconds())
	}()

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

	env, err := aep.DecodeLineMinimal(data)
	if err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, "malformed message: "+err.Error())
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
		return nil, InitData{}, err
	}

	if env.Event.Type != events.Init {
		c.sendInitError(events.ErrCodeProtocolViolation, "expected init as first message, got "+string(env.Event.Type))
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeProtocolViolation))))
		return nil, InitData{}, fmt.Errorf("expected init, got %s", env.Event.Type)
	}

	observability.GatewayMessages().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("direction", "incoming"), attribute.String("event_type", string(events.Init))))

	initData, initErr := ValidateInit(env)
	if initErr != nil {
		c.sendInitError(initErr.Code, initErr.Message)
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(initErr.Code))))
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
			c.emitAudit(audit.ActionAuthDenied, audit.OutcomeDenied, "", platformWebChat, c.sessionID, c.RemoteAddr())
			return fmt.Errorf("deferred auth: no token in init envelope")
		}
		uid, ok := auth.AuthenticateKey(context.Background(), initData.Auth.Token)
		if !ok {
			c.sendInitError(events.ErrCodeUnauthorized, "invalid token")
			// Invalid token = no valid credential presented → denied (not a
			// presented-then-rejected case, which would be apikey_used failure).
			c.emitAudit(audit.ActionAuthDenied, audit.OutcomeDenied, "", platformWebChat, c.sessionID, c.RemoteAddr())
			return fmt.Errorf("deferred auth: invalid token")
		}
		c.userID = uid
		c.pendingAuth = false
		// Per-connection cookie/token re-validation emits no success row; the
		// session.create + message.inbound rows carry attribution. Mirrors the
		// HTTP path (security/auth.go AuthenticateRequest).
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

	var workspaceID string
	if initData.WorkspaceID != "" {
		if c.starter == nil {
			workspaceID = initData.WorkspaceID
		} else {
			ws, err := c.starter.GetWorkspaceByID(context.Background(), initData.WorkspaceID)
			if err != nil {
				c.sendInitError(events.ErrCodeInvalidMessage, "workspace not found")
				observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
				return "", nil, err
			}
			if ws.Status == "disabled" {
				c.sendInitError(events.ErrCodeInvalidMessage, "workspace is disabled")
				observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
				return "", nil, fmt.Errorf("init: workspace %s is disabled", initData.WorkspaceID)
			}
			// Owner check: workspace is accessible only to its owner. The
			// "anonymous" carve-out is a dev-mode path — anonymous-owned
			// workspaces (uid="anonymous", created when no idp is configured)
			// are treated as platform-shared so dev WS clients can bind to
			// them. Production configures idp so uid is never "anonymous" and
			// this branch is unreachable; it MUST NOT be extended to real
			// identities. REST workspace handlers do not carry this carve-out
			// (they require a resolved owner via AuthenticateRequest). See
			// PR #773 review P3 for the security-boundary rationale.
			if ws.OwnerUserID != "anonymous" && ws.OwnerUserID != c.userID {
				c.sendInitError(events.ErrCodeInvalidMessage, "workspace access denied")
				observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
				return "", nil, fmt.Errorf("init: workspace %s access denied for user %s", initData.WorkspaceID, c.userID)
			}
			workDir = ws.WorkDir
			workspaceID = ws.ID
		}
	}
	c.workspaceID = workspaceID

	expanded, err := validateAndExpandWorkDir(workDir)
	if err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, err.Error())
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
		return "", nil, err
	}
	workDir = expanded

	// Reject "|" in the client-provided session_id: it flows into
	// DeriveSessionKey's hash name and would alias session keys (review P3).
	if verr := session.ValidateClientKey(env.SessionID); verr != nil {
		c.sendInitError(events.ErrCodeProtocolViolation, "session_id must not contain '|'")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeProtocolViolation))))
		return "", nil, fmt.Errorf("init: invalid session_id: %w", verr)
	}

	var sessionID string
	var preResolved *session.SessionInfo
	if env.SessionID != "" {
		if existing, getErr := sm.Get(context.Background(), env.SessionID); getErr == nil && existing != nil && existing.State != events.StateDeleted {
			sessionID = env.SessionID
			preResolved = existing
		}
	}
	if sessionID == "" {
		sessionID = session.DeriveSessionKey(c.userID, initData.WorkerType, env.SessionID, workspaceID, workDir)
	}

	if preResolved != nil && preResolved.WorkspaceID != workspaceID {
		c.sendInitError(events.ErrCodeInvalidMessage, "session workspace mismatch")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInvalidMessage))))
		return "", nil, fmt.Errorf("init: session %s workspace mismatch", sessionID)
	}

	if !c.hub.InitThrottle.Check(sessionID) {
		c.sendInitError(events.ErrCodeRateLimited, "too many failed attempts, please back off")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeRateLimited))))
		return "", nil, fmt.Errorf("init throttled for session %s", sessionID)
	}

	c.mu.Lock()
	c.initDone = false
	c.mu.Unlock()

	c.hub.LeaveSession("", c)
	c.hub.JoinSession(sessionID, c)
	// Hydrate SeqGen from persisted events so the upcoming init_ack and worker
	// events continue monotonically instead of restarting from 1 (issue #879:
	// WS disconnect deleted the counter → reconnect collided with persisted seq
	// segments and buried new events under ORDER BY seq DESC). Runs after
	// JoinSession and before resolveSessionState starts the worker (which assigns
	// seqs in a goroutine) and before finalizeInit's init_ack NextSeq.
	c.hub.EnsureSeqHydrated(sessionID)

	return c.resolveSessionState(sessionID, initData, workDir, sm, preResolved, env.SessionID)
}

// resolveSessionState handles the session state machine transitions:
// not-found → create, created → start, deleted → recreate,
// idle/terminated → resume (with fresh-start fallback), running+alive → fast reconnect.
func (c *Conn) resolveSessionState(sessionID string, initData InitData, workDir string, sm connSM, preResolved *session.SessionInfo, clientKey string) (string, *session.SessionInfo, error) {
	var si *session.SessionInfo
	var err error

	if preResolved != nil {
		si = preResolved
	} else {
		si, err = sm.Get(context.Background(), sessionID)
	}

	if err != nil {
		result, handleErr := c.handleSessionNotFound(sessionID, initData, workDir, sm, err, clientKey)
		return sessionID, result, handleErr
	}

	// Preserve original client_key from existing session to prevent
	// overwrite when reconnecting with the server-assigned session_id
	// (webchat stores the server UUID in localStorage, not the original
	// client key like "main" or "sess_xxx").
	if si.ClientKey != "" {
		clientKey = si.ClientKey
	}

	switch si.State {
	case events.StateCreated:
		result, stateErr := c.startCreatedSession(sessionID, initData, workDir, sm, si, clientKey)
		return sessionID, result, stateErr
	case events.StateDeleted:
		result, stateErr := c.recreateDeletedSession(sessionID, initData, workDir, sm, clientKey)
		return sessionID, result, stateErr
	default:
		result, stateErr := c.handleExistingSession(sessionID, workDir, sm, si, initData, clientKey)
		return sessionID, result, stateErr
	}
}

func (c *Conn) handleSessionNotFound(sessionID string, initData InitData, workDir string, sm connSM, lookupErr error, clientKey string) (*session.SessionInfo, error) {
	if errors.Is(lookupErr, session.ErrSessionCleanupPending) {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeSessionBusy, "session cleanup in progress; retry later")
		return nil, fmt.Errorf("get session: %w", lookupErr)
	}
	if !errors.Is(lookupErr, session.ErrSessionNotFound) {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, lookupErr.Error())
		return nil, fmt.Errorf("get session: %w", lookupErr)
	}

	if c.starter != nil {
		if err := c.starter.StartSession(context.Background(), worker.SessionStartParams{
			ID:           sessionID,
			UserID:       c.userID,
			BotID:        c.botID,
			WorkerType:   initData.WorkerType,
			AllowedTools: initData.Config.AllowedTools,
			WorkDir:      workDir,
			Platform:     platformWebChat,
			Title:        initData.Title,
			ClientKey:    clientKey,
			WorkspaceID:  c.workspaceID,
		}); err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, "failed to create session")
			observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
			return nil, fmt.Errorf("create session: %w", err)
		}
		si, err := sm.Get(context.Background(), sessionID)
		if err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, "session not found after creation")
			observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
			return nil, fmt.Errorf("get session after start: %w", err)
		}
		return si, nil
	}

	// Test mode: create directly via session manager.
	si, err := sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, "", initData.WorkerType, initData.Config.AllowedTools, platformWebChat, nil, workDir, initData.Title, clientKey)
	if err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, "failed to create session")
		return nil, fmt.Errorf("create session: %w", err)
	}
	return si, nil
}

func (c *Conn) startCreatedSession(sessionID string, initData InitData, workDir string, sm connSM, si *session.SessionInfo, clientKey string) (*session.SessionInfo, error) {
	if c.starter == nil {
		return si, nil // no starter in test mode, session stays CREATED
	}
	if err := c.starter.StartSession(context.Background(), worker.SessionStartParams{
		ID:           sessionID,
		UserID:       c.userID,
		BotID:        c.botID,
		WorkerType:   initData.WorkerType,
		AllowedTools: initData.Config.AllowedTools,
		WorkDir:      workDir,
		Platform:     platformWebChat,
		Title:        initData.Title,
		ClientKey:    clientKey,
		WorkspaceID:  c.workspaceID,
	}); err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, "failed to start session")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
		return nil, fmt.Errorf("start unstarted session: %w", err)
	}
	si, err := sm.Get(context.Background(), sessionID)
	if err != nil {
		c.sendInitError(events.ErrCodeInternalError, "session lost after creation")
		return nil, fmt.Errorf("get session after start: %w", err)
	}
	return si, nil
}

func (c *Conn) recreateDeletedSession(sessionID string, initData InitData, workDir string, sm connSM, clientKey string) (*session.SessionInfo, error) {
	_ = sm.DeletePhysical(context.Background(), sessionID)
	if c.starter == nil {
		// Test mode: re-create session directly since the old one was physically deleted.
		newSI, err := sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, "", initData.WorkerType, initData.Config.AllowedTools, platformWebChat, nil, workDir, initData.Title, clientKey)
		if err != nil {
			return nil, fmt.Errorf("recreate deleted session (test mode): %w", err)
		}
		return newSI, nil
	}
	if err := c.starter.StartSession(context.Background(), worker.SessionStartParams{
		ID:           sessionID,
		UserID:       c.userID,
		BotID:        c.botID,
		WorkerType:   initData.WorkerType,
		AllowedTools: initData.Config.AllowedTools,
		WorkDir:      workDir,
		Platform:     platformWebChat,
		Title:        initData.Title,
		ClientKey:    clientKey,
		WorkspaceID:  c.workspaceID,
	}); err != nil {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeInternalError, fmt.Sprintf("failed to recreate deleted session: %v", err))
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
		return nil, fmt.Errorf("recreate deleted session: %w", err)
	}
	si, err := sm.Get(context.Background(), sessionID)
	if err != nil {
		c.sendInitError(events.ErrCodeInternalError, "session lost after recreation")
		return nil, fmt.Errorf("get session after recreation: %w", err)
	}
	return si, nil
}

func (c *Conn) handleExistingSession(sessionID, workDir string, sm connSM, si *session.SessionInfo, initData InitData, clientKey string) (*session.SessionInfo, error) {
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

	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer resumeCancel()
	resumeErr := c.starter.ResumeSession(resumeCtx, sessionID, workDir)
	if resumeErr != nil {
		if errors.Is(resumeErr, worker.ErrResumeCheckFailed) {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, "unable to verify the previous worker session; retry later")
			return nil, fmt.Errorf("resume verification failed: %w", resumeErr)
		}
		startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer startCancel()
		if err := c.starter.StartSession(startCtx, worker.SessionStartParams{
			ID:           sessionID,
			UserID:       c.userID,
			BotID:        c.botID,
			WorkerType:   si.WorkerType,
			AllowedTools: initData.Config.AllowedTools,
			WorkDir:      workDir,
			Platform:     platformWebChat,
			Title:        initData.Title,
			ClientKey:    clientKey,
		}); err != nil {
			c.hub.InitThrottle.RecordFailure(sessionID)
			msg := fmt.Sprintf("resume failed (%v), then start also failed (%v)", resumeErr, err)
			c.sendInitError(events.ErrCodeInternalError, msg)
			observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
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
func (c *Conn) finalizeInit(sessionID string, si *session.SessionInfo, _ InitData, span trace.Span) error {
	// SEC-008: reject cross-user access on reconnect.
	if c.userID != "" && si.UserID != "" && c.userID != si.UserID {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeUnauthorized, "user_id mismatch")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeUnauthorized))))
		return fmt.Errorf("user_id mismatch: connection=%s session=%s", c.userID, si.UserID)
	}

	// SEC-007: reject cross-bot access.
	if c.botID != "" && si.BotID != "" && c.botID != si.BotID {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeUnauthorized, "bot_id mismatch")
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeUnauthorized))))
		return fmt.Errorf("bot_id mismatch: connection=%s session=%s", c.botID, si.BotID)
	}

	c.hub.InitThrottle.RecordSuccess(sessionID)

	c.mu.Lock()
	c.sessionID = sessionID
	c.userID = si.UserID
	c.mu.Unlock()

	ack := BuildInitAck(sessionID, si.State, si.WorkerType)
	ack.Seq = c.hub.NextSeq(sessionID)
	if err := c.WriteCtx(context.Background(), ack); err != nil {
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", string(events.ErrCodeInternalError))))
		return fmt.Errorf("send init_ack: %w", err)
	}
	observability.GatewayMessages().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("direction", "outgoing"), attribute.String("event_type", InitAck)))

	c.markInitDone()

	c.log.Info("gateway: init complete", "session_id", sessionID,
		"worker_type", si.WorkerType, "state", si.State)
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
	data, err := aep.EncodeJSON(env)
	if err != nil {
		return err
	}
	return c.writeDispatch(data, env.Event.Type)
}

// RouteWriteData writes pre-encoded JSON bytes to this connection.
// The caller (typically Hub.routeMessage) encodes once and distributes
// the same bytes to all subscribed connections.
func (c *Conn) RouteWriteData(data []byte, eventType events.Kind) error {
	return c.writeDispatch(data, eventType)
}

// writeDispatch is the shared write-path for both RouteWrite and RouteWriteData.
// It handles metrics, init-phase buffering, and droppable vs reliable dispatch.
//
// *Conn is always webchat. webchat deltas are delivered via the guaranteed
// path (sendData): WS clients are local and a drop is silent data loss with
// no recovery signal — a slow client should be disconnected (and reconnect)
// rather than lose mid-stream text. Only Raw remains droppable here, since it
// is a fire-and-forget passthrough, not streaming user text. Platform (feishu/
// slack) deltas stay droppable via their own pcEntry.WriteCtx path.
func (c *Conn) writeDispatch(data []byte, eventType events.Kind) error {
	observability.GatewayMessages().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("direction", "outgoing"), attribute.String("event_type", string(eventType))))
	if handled, err := c.bufferOrReject(data); handled {
		return err
	}
	if eventType == events.Raw {
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
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", "slow_client")))
		_ = c.Close()
		return errors.New("write channel full, slow client disconnected")
	}
}

// trySendData attempts to write pre-encoded data without blocking.
// Silently drops the message if the channel is full (for Raw events on
// webchat — fire-and-forget passthrough). Platform deltas stay droppable
// via their own pcEntry.WriteCtx path; webchat deltas use the guaranteed
// sendData path (slow client disconnects and replays from the event store).
func (c *Conn) trySendData(data []byte) error {
	select {
	case c.writeCh <- data:
		return nil
	default:
		observability.GatewayDeltasDropped().Add(c.hub.ctx, 1)
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
		observability.GatewayErrors().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("error_code", "slow_client")))
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
	needClose := false
flushLoop:
	for _, data := range c.initPending {
		if c.closed {
			break
		}
		select {
		case c.writeCh <- data:
		default:
			c.log.Warn("gateway: init flush write channel full", "session_id", c.sessionID)
			needClose = true
			break flushLoop
		}
	}
	c.initPending = nil
	c.mu.Unlock()
	// 锁外调 Close：Close 内部获取 c.mu，sync.Mutex 不可重入，持锁调用会自死锁。
	if needClose {
		_ = c.Close()
	}
}

// PreferEnvelope returns false: WebSocket connections benefit from pre-encoded
// bytes (RouteWriteData) for efficiency.
func (c *Conn) PreferEnvelope() bool { return false }

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
	observability.GatewayMessages().Add(c.hub.ctx, 1, metric.WithAttributes(attribute.String("direction", "outgoing"), attribute.String("event_type", string(events.Error))))
	_ = c.WriteCtx(context.Background(), env)
}
