package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/config"
	"hotplex-worker/internal/metrics"
	"hotplex-worker/internal/security"
	"hotplex-worker/internal/session"
	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log          *slog.Logger
	cfg          *config.Config
	hub          *Hub
	sm           *session.Manager
	jwtValidator *security.JWTValidator
}

// NewHandler creates a new message handler.
func NewHandler(log *slog.Logger, cfg *config.Config, hub *Hub, sm *session.Manager, jwtValidator *security.JWTValidator) *Handler {
	return &Handler{
		log:          log,
		cfg:          cfg,
		hub:          hub,
		sm:           sm,
		jwtValidator: jwtValidator,
	}
}

// Handle processes an incoming envelope from a client.
func (h *Handler) Handle(ctx context.Context, env *events.Envelope) error {
	switch env.Event.Type {
	case events.Input:
		return h.handleInput(ctx, env)
	case events.Ping:
		return h.handlePing(ctx, env)
	case events.Control:
		return h.handleControl(ctx, env)
	// AEP-011 / AEP-012: pass-through events from worker to all session clients.
	case events.Reasoning:
		// AEP-011: reasoning 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("reasoning", "s2c").Inc()
	case events.Step:
		// AEP-011: step 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("step", "s2c").Inc()
	case events.PermissionRequest:
		// AEP-011: permission_request 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("permission_request", "s2c").Inc()
	case events.PermissionResponse:
		// AEP-011: permission_response 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("permission_response", "s2c").Inc()
	case events.Message:
		// AEP-012: message 完整消息事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("message", "s2c").Inc()
	case events.MessageStart:
		// AEP-012: message.start 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("message.start", "s2c").Inc()
	case events.MessageEnd:
		// AEP-012: message.end 事件透传
		if err := h.hub.SendToSession(ctx, env); err != nil {
			return err
		}
		metrics.GatewayEventsTotal.WithLabelValues("message.end", "s2c").Inc()
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown event type: %s", env.Event.Type)
	}
	return nil
}

func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
	}

	content, _ := data["content"].(string)

	// Check SESSION_BUSY: input and state transition must be atomic.
	si, err := h.sm.Get(env.SessionID)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	if !si.State.IsActive() {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session not active: %s", si.State)
	}

	// Atomic transition + input.
	if err := h.sm.TransitionWithInput(ctx, env.SessionID, events.StateRunning, content, nil); err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session busy: %v", err)
	}

	// Deliver to worker.
	w := h.sm.GetWorker(env.SessionID)
	if w != nil {
		if err := w.Input(ctx, content, nil); err != nil {
			h.log.Warn("gateway: worker input", "err", err, "session_id", env.SessionID)
		}
	}

	return nil
}

func (h *Handler) handlePing(ctx context.Context, env *events.Envelope) error {
	// Include current session state in pong (per AEP spec §11.4).
	si, err := h.sm.Get(env.SessionID)
	state := "unknown"
	if err == nil {
		state = string(si.State)
	}

	reply := events.NewEnvelope(
		aep.NewID(),
		env.SessionID,
		h.hub.NextSeq(env.SessionID),
		events.Pong,
		map[string]any{"state": state},
	)
	return h.hub.SendToSession(ctx, reply)
}

// handleControl processes client-originated control messages (terminate, delete).
// Server-originated control messages (reconnect, session_invalid, throttle) are
// sent via SendControlToSession.
func (h *Handler) handleControl(ctx context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "control: invalid data")
	}

	action, _ := data["action"].(string)
	h.log.Info("gateway: control received", "action", action, "session_id", env.SessionID)

	switch events.ControlAction(action) {
	case events.ControlActionTerminate:
		// Ownership check: only the session owner can terminate.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Transition to TERMINATED and kill the worker.
		if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "client_kill"); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "terminate failed: %v", err)
		}
		// Send error + done to client.
		errEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
			Code:    events.ErrCodeSessionTerminated,
			Message: "session terminated by client",
		})
		doneEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Done, events.DoneData{
			Success: false,
		})
		_ = h.hub.SendToSession(ctx, errEnv)
		_ = h.hub.SendToSession(ctx, doneEnv)
		return nil

	case events.ControlActionDelete:
		// Ownership check: only the session owner can delete.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Delete the session (bypasses TERMINATED state per design §5).
		if err := h.sm.Delete(ctx, env.SessionID); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "delete failed: %v", err)
		}
		return nil

	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown control action: %s", action)
	}
}

// SendControlToSession sends a server-originated control message to the client.
// Used for reconnect, session_invalid, and throttle notifications.
func (h *Handler) SendControlToSession(ctx context.Context, sessionID string, action events.ControlAction, reason string, details map[string]any) error {
	env := events.NewEnvelope(aep.NewID(), sessionID, h.hub.NextSeq(sessionID), events.Control, events.ControlData{
		Action:  action,
		Reason:  reason,
		Details: details,
	})
	env.Priority = events.PriorityControl // control messages bypass backpressure
	return h.hub.SendToSession(ctx, env)
}

// SendReconnect sends a reconnect control message to the client.
func (h *Handler) SendReconnect(ctx context.Context, sessionID, reason string, delayMs int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionReconnect, reason, map[string]any{
		"delay_ms": delayMs,
	})
}

// SendSessionInvalid sends a session_invalid control message to the client.
func (h *Handler) SendSessionInvalid(ctx context.Context, sessionID, reason string, recoverable bool) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionSessionInvalid, reason, map[string]any{
		"recoverable": recoverable,
	})
}

// SendThrottle sends a throttle control message to the client.
func (h *Handler) SendThrottle(ctx context.Context, sessionID string, backoffMs int, maxMessageRate int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionThrottle, "rate limit exceeded", map[string]any{
		"suggestion": map[string]any{
			"max_message_rate": maxMessageRate,
		},
		"backoff_ms":  backoffMs,
		"retry_after": backoffMs,
	})
}

func (h *Handler) sendErrorf(ctx context.Context, env *events.Envelope, code events.ErrorCode, format string, args ...any) error {
	err := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	_ = h.hub.SendToSession(ctx, err) // best-effort; always return the error
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, args...))
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// SessionManager abstracts the session.Manager methods used by Bridge.
// It allows Bridge to be tested without a real Manager instance.
type SessionManager interface {
	Create(ctx context.Context, id, userID string, wt worker.WorkerType, allowedTools []string) (*session.SessionInfo, error)
	AttachWorker(id string, w worker.Worker) error
	DetachWorker(id string)
	Transition(ctx context.Context, id string, to events.SessionState) error
	Get(id string) (*session.SessionInfo, error)
	Delete(ctx context.Context, id string) error
}

// WorkerFactory creates worker instances. Production code uses defaultWorkerFactory.
type WorkerFactory interface {
	NewWorker(t worker.WorkerType) (worker.Worker, error)
}

type defaultWorkerFactory struct{}

func (defaultWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	return worker.NewWorker(t)
}
