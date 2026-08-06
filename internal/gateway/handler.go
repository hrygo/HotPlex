package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// LevelTrace is one step below slog.LevelDebug, for high-volume protocol
// chatter (ping/pong) that should not appear even at debug level.
const LevelTrace = slog.Level(-8)

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log             *slog.Logger
	hub             *Hub
	sm              SessionManager
	auth            *security.Authenticator
	bridge          *Bridge
	skillsLocator   SkillsLocator
	auditCollector  *audit.Collector
	executionStore  execution.Store
	repairer        *execution.Repairer
	ownerInstanceID string
	stopFence       turnStopFence
}

// SkillsLocator discovers skills from the filesystem.
type SkillsLocator interface {
	List(ctx context.Context, homeDir, workDir string) ([]skills.Skill, error)
	Close()
}

// NewHandler creates a new message handler.
func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		log:             deps.Log.With("component", "handler"),
		hub:             deps.Hub,
		sm:              deps.SM,
		auth:            deps.Auth,
		bridge:          deps.Bridge,
		skillsLocator:   deps.SkillsLocator,
		executionStore:  deps.ExecutionStore,
		repairer:        deps.Repairer,
		ownerInstanceID: deps.OwnerInstanceID,
		// stopFence stays zero-valued: the turn stop fence is ready to use.
	}
}

// SetAuditCollector injects the audit collector for message event recording.
func (h *Handler) SetAuditCollector(ac *audit.Collector) {
	h.auditCollector = ac
}

// emitAudit enqueues a non-blocking message.inbound audit event. No-op when collector is nil.
func (h *Handler) emitAudit(outcome, userID, platform, sessionID, content string) {
	if h.auditCollector == nil {
		return
	}
	if userID == "" {
		userID = audit.AnonymousUserID
	}
	detailJSON := "{}"
	if content != "" {
		detail := map[string]any{"content": content}
		if bytes, err := json.Marshal(detail); err == nil {
			detailJSON = string(bytes)
		}
	}
	_ = h.auditCollector.Enqueue(context.Background(), &audit.UserActivity{
		Ts:         time.Now().UnixMilli(),
		UserID:     userID,
		UserIDType: audit.UserIDTypePlatform,
		Platform:   platform,
		SessionID:  sessionID,
		Action:     audit.ActionMessageInbound,
		Outcome:    outcome,
		DetailJSON: detailJSON,
	})
}

// emitInteractionAudit enqueues a non-blocking interaction response audit event
// (permission.response / question.response / elicitation.response).
func (h *Handler) emitInteractionAudit(userID, platform, sessionID string, eventType events.Kind, data any, content string) {
	if h.auditCollector == nil {
		return
	}
	if userID == "" {
		userID = audit.AnonymousUserID
	}

	var action string
	var resourceType string
	var resourceID string
	var outcome string
	detailMap := make(map[string]any)

	switch eventType {
	case events.PermissionRequest:
		action = audit.ActionPermissionRequest
		resourceType = "permission"
		var reqID string
		var toolName string
		var description string
		var args []string

		if prd, ok := data.(events.PermissionRequestData); ok {
			reqID = prd.ID
			toolName = prd.ToolName
			description = prd.Description
			args = prd.Args
		} else if prdPtr, ok := data.(*events.PermissionRequestData); ok && prdPtr != nil {
			reqID = prdPtr.ID
			toolName = prdPtr.ToolName
			description = prdPtr.Description
			args = prdPtr.Args
		} else if m, ok := data.(map[string]any); ok {
			reqID, _ = m["id"].(string)
			if reqID == "" {
				reqID, _ = m["request_id"].(string)
			}
			toolName, _ = m["tool_name"].(string)
			if toolName == "" {
				toolName, _ = m["tool"].(string)
			}
			description, _ = m["description"].(string)
			if rawArgs, ok := m["args"].([]string); ok {
				args = rawArgs
			} else if rawArgsAny, ok := m["args"].([]any); ok {
				for _, a := range rawArgsAny {
					if s, ok := a.(string); ok {
						args = append(args, s)
					}
				}
			}
		}

		resourceID = reqID
		if reqID != "" {
			detailMap["id"] = reqID
		}
		if toolName != "" {
			detailMap["tool_name"] = toolName
		}
		if description != "" {
			detailMap["description"] = description
		}
		if len(args) > 0 {
			detailMap["args"] = args
		}
		outcome = audit.OutcomeSuccess

	case events.PermissionResponse:
		action = audit.ActionPermissionResponse
		resourceType = "permission"
		var allowed = true
		var reason string
		var reqID string

		if prd, ok := data.(events.PermissionResponseData); ok {
			reqID = prd.ID
			allowed = prd.Allowed
			reason = prd.Reason
		} else if m, ok := data.(map[string]any); ok {
			reqID, _ = m["id"].(string)
			if reqID == "" {
				reqID, _ = m["request_id"].(string)
			}
			if a, ok := m["allowed"].(bool); ok {
				allowed = a
			}
			reason, _ = m["reason"].(string)
		}

		resourceID = reqID
		if reqID != "" {
			detailMap["id"] = reqID
		}
		detailMap["allowed"] = allowed
		if reason != "" {
			detailMap["reason"] = reason
		}
		if content != "" {
			detailMap["content"] = content
		}
		if allowed {
			outcome = audit.OutcomeSuccess
		} else {
			outcome = audit.OutcomeDenied
		}

	case events.QuestionResponse:
		action = audit.ActionQuestionResponse
		resourceType = "question"
		var reqID string
		var answers any

		if qrd, ok := data.(events.QuestionResponseData); ok {
			reqID = qrd.ID
			answers = qrd.Answers
		} else if m, ok := data.(map[string]any); ok {
			reqID, _ = m["id"].(string)
			answers = m["answers"]
		}

		resourceID = reqID
		if reqID != "" {
			detailMap["id"] = reqID
		}
		if answers != nil {
			detailMap["answers"] = answers
		}
		if content != "" {
			detailMap["content"] = content
		}
		outcome = audit.OutcomeSuccess

	case events.ElicitationResponse:
		action = audit.ActionElicitationResponse
		resourceType = "elicitation"
		var reqID string
		var act string
		var elicitContent any

		if erd, ok := data.(events.ElicitationResponseData); ok {
			reqID = erd.ID
			act = erd.Action
			elicitContent = erd.Content
		} else if m, ok := data.(map[string]any); ok {
			reqID, _ = m["id"].(string)
			act, _ = m["action"].(string)
			elicitContent = m["content"]
		}

		resourceID = reqID
		if reqID != "" {
			detailMap["id"] = reqID
		}
		if act != "" {
			detailMap["action"] = act
		}
		if elicitContent != nil {
			detailMap["content"] = elicitContent
		} else if content != "" {
			detailMap["content"] = content
		}
		if act == "decline" || act == "cancel" {
			outcome = audit.OutcomeDenied
		} else {
			outcome = audit.OutcomeSuccess
		}

	default:
		return
	}

	detailJSON := "{}"
	if len(detailMap) > 0 {
		if bytes, err := json.Marshal(detailMap); err == nil {
			detailJSON = string(bytes)
		}
	}

	_ = h.auditCollector.Enqueue(context.Background(), &audit.UserActivity{
		Ts:           time.Now().UnixMilli(),
		UserID:       userID,
		UserIDType:   audit.UserIDTypePlatform,
		Platform:     platform,
		SessionID:    sessionID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Outcome:      outcome,
		DetailJSON:   detailJSON,
	})
}

// Handle processes an incoming envelope from a client.
func (h *Handler) Handle(ctx context.Context, env *events.Envelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			sid := ""
			if env != nil {
				sid = env.SessionID
			}
			h.log.Error("gateway: panic in handler", "session_id", sid, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	if env.Event.Type != events.Ping {
		dataSize, dataSHA256 := eventDataLogSummary(env.Event.Data)
		h.log.Info("gateway: Handle received event",
			"type", env.Event.Type,
			"session_id", env.SessionID,
			"seq", env.Seq,
			"data_size", dataSize,
			"data_sha256", dataSHA256,
		)
	}
	switch env.Event.Type {
	case events.Input:
		return h.handleInput(ctx, env)
	case events.Ping:
		return h.handlePing(ctx, env)
	case events.Control:
		return h.handleControl(ctx, env)
	case events.WorkerCmd:
		return h.handleWorkerCommand(ctx, env)
	case events.PermissionResponse, events.QuestionResponse, events.ElicitationResponse:
		return h.handleInteractionResponseEvent(ctx, env)
	// AEP-011 / AEP-012: pass-through events from worker to all session clients.
	case events.Reasoning, events.Step, events.PermissionRequest,
		events.QuestionRequest,
		events.ElicitationRequest,
		events.Message, events.MessageStart, events.MessageEnd:
		return h.passthroughToSession(ctx, env)
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown event type: %s", env.Event.Type)
	}
}

// eventDataLogSummary returns an operationally useful, non-plaintext summary
// of event data. JSON encoding gives maps a stable key order; unsupported
// values use their type name, which is likewise stable and reveals no body.
func eventDataLogSummary(data any) (int, string) {
	encoded, err := json.Marshal(data)
	if err != nil {
		encoded = []byte(fmt.Sprintf("unsupported:%T", data))
	}
	sum := sha256.Sum256(encoded)
	return len(encoded), hex.EncodeToString(sum[:])[:16]
}

func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		h.log.Warn("gateway: handleInput malformed data", "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
	}

	if handled, err := h.tryInteractionResponse(ctx, env, data); handled {
		return err
	}

	content, _ := data["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if handled, err := h.tryCommandDispatch(ctx, env, content); handled {
		// Control/help/worker commands bypass the durable execution path, so
		// no InputAck or Done is ever sent. The webchat client locks
		// pendingInput on every sendInput and only releases on a terminal
		// InputAck/Done/Error — without this synthetic "delivered" ack the UI
		// stays frozen for up to the 5-minute settle timeout after commands
		// like /reset that only emit a State event.
		if err == nil {
			h.ackControlCommand(ctx, env)
		}
		return err
	}

	return h.deliverToWorker(ctx, env, content)
}

func (h *Handler) cancelRetryIfNeeded(sessionID string) {
	if h.bridge != nil {
		h.bridge.CancelRetry(sessionID)
	}
}

// tryInteractionResponse routes permission/question/elicitation responses directly
// to the worker, bypassing command detection and state transitions.
func (h *Handler) tryInteractionResponse(ctx context.Context, env *events.Envelope, data map[string]any) (bool, error) {
	md, ok := data["metadata"].(map[string]any)
	if !ok {
		return false, nil
	}
	if md["permission_response"] == nil &&
		md["question_response"] == nil &&
		md["elicitation_response"] == nil {
		return false, nil
	}
	h.cancelRetryIfNeeded(env.SessionID)

	respType := "unknown"
	switch {
	case md["permission_response"] != nil:
		respType = "permission"
	case md["question_response"] != nil:
		respType = "question"
	case md["elicitation_response"] != nil:
		respType = "elicitation"
	}

	content, _ := data["content"].(string)
	w := h.sm.GetWorker(env.SessionID)
	if w != nil {
		h.log.Info("gateway: routing interaction response",
			"type", respType,
			"session_id", env.SessionID)
		if err := w.Input(ctx, content, md); err != nil {
			h.log.Warn("gateway: worker interaction response failed",
				"err", err,
				"type", respType,
				"session_id", env.SessionID)
			return true, fmt.Errorf("gateway: %s interaction response failed: %w", respType, err)
		} else if h.bridge != nil {
			h.bridge.CaptureInboundEvent(env.SessionID, env.Seq, events.Input, env.Event.Data)
			h.recordPermissionDenial(respType, md, env)
		}
		if h.auditCollector != nil {
			siPlatform := ""
			if h.sm != nil {
				if si, err := h.sm.Get(ctx, env.SessionID); err == nil {
					siPlatform = si.Platform
				}
			}
			switch respType {
			case "permission":
				h.emitInteractionAudit(env.OwnerID, siPlatform, env.SessionID, events.PermissionResponse, md["permission_response"], content)
			case "question":
				h.emitInteractionAudit(env.OwnerID, siPlatform, env.SessionID, events.QuestionResponse, md["question_response"], content)
			case "elicitation":
				h.emitInteractionAudit(env.OwnerID, siPlatform, env.SessionID, events.ElicitationResponse, md["elicitation_response"], content)
			}
		}
	} else {
		h.log.Warn("gateway: interaction response dropped — no worker",
			"type", respType,
			"session_id", env.SessionID)
		return true, fmt.Errorf("gateway: %s interaction response dropped: no worker for session %s", respType, env.SessionID)
	}
	return true, nil
}

// recordPermissionDenial registers a user's tool denial in the bridge dedup
// cache so a same-fingerprint retry within the window is auto-suppressed.
// Watchdog auto-denials (reason "interaction timed out") are excluded — a
// timeout is not a user decision, and the user deserves a fresh card on retry.
func (h *Handler) recordPermissionDenial(respType string, md map[string]any, env *events.Envelope) {
	if respType != "permission" {
		return
	}
	pr, ok := md["permission_response"].(map[string]any)
	if !ok {
		return
	}
	if allowed, _ := pr["allowed"].(bool); allowed {
		return
	}
	if reason, _ := pr["reason"].(string); reason == "interaction timed out" {
		return
	}
	h.bridge.RecordPermissionDeny(env.SessionID, env.ID, env.OwnerID)
}

// tryCommandDispatch detects help/control/worker commands and dispatches them.
// Returns (true, err) if a command was handled, (false, nil) to fall through.
func (h *Handler) tryCommandDispatch(ctx context.Context, env *events.Envelope, content string) (handled bool, err error) {
	if messaging.IsHelpCommand(content) {
		h.cancelRetryIfNeeded(env.SessionID)
		helpEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID,
			0,
			events.Message, events.MessageData{Content: messaging.HelpText()},
		)
		return true, h.hub.SendToSession(ctx, helpEnv)
	}

	if result := messaging.ParseControlCommand(content); result != nil {
		h.cancelRetryIfNeeded(env.SessionID)
		data := events.ControlData{Action: result.Action}
		if result.Arg != "" {
			data.Details = map[string]any{"path": result.Arg}
		}
		ctrlEnv := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: env.SessionID,
			Seq:       h.hub.NextSeq(env.SessionID),
			Event: events.Event{
				Type: events.Control,
				Data: data,
			},
			OwnerID: env.OwnerID,
		}
		return true, h.handleControl(ctx, ctrlEnv)
	}

	if cmdResult := messaging.ParseWorkerCommand(content); cmdResult != nil {
		h.cancelRetryIfNeeded(env.SessionID)
		wcmdEnv := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: env.SessionID,
			Seq:       h.hub.NextSeq(env.SessionID),
			Event: events.Event{
				Type: events.WorkerCmd,
				Data: events.WorkerCommandData{
					Command: cmdResult.Command,
					Args:    cmdResult.Args,
					Extra:   cmdResult.Extra,
				},
			},
			OwnerID: env.OwnerID,
		}
		return true, h.handleWorkerCommand(ctx, wcmdEnv)
	}

	return false, nil
}

// handleSupplementOnBusy routes a SESSION_BUSY input to mid-turn passthrough
// (when the worker implements MidTurnInjector and the turn is still live) or
// the fallback pending buffer. Falls back to the legacy SESSION_BUSY error
// only when bridge buffering is unavailable.
//
// Decision order:
//  1. Worker implements MidTurnInjector AND !IsStopped → InjectMidTurn.
//     Success → metric + capture + "injected" notify + return nil.
//     Failure → re-check the gate; deliver as a new turn if released, otherwise
//     fall through to the buffer.
//  2. bridge != nil → BufferPending + metric + "buffered" notify + return nil.
//  3. Otherwise → legacy sendErrorf SESSION_BUSY.
func (h *Handler) handleSupplementOnBusy(ctx context.Context, env *events.Envelope, content string) error {
	if h.bridge == nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session has an active execution")
	}
	unlockSession, ok := h.bridge.lockSupplementSession(env.SessionID)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionTerminated, "gateway is shutting down")
	}
	sessionLocked := true
	defer func() {
		if sessionLocked {
			unlockSession()
		}
	}()

	clientID := clientMessageID(env)
	payloadHash, err := inputPayloadHash(env)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "cannot hash supplement payload")
	}
	lease, disposition := h.bridge.BeginSupplement(env.SessionID, clientID, payloadHash)
	switch disposition {
	case supplementConflict:
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage,
			"client message id %q was already used with different input", clientID)
	case supplementCapacity:
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "too many concurrent supplement attempts")
	case supplementInjected, supplementBuffered:
		h.ackSupplement(ctx, env, "", true)
		return nil
	case supplementNormal:
		unlockSession()
		sessionLocked = false
		return h.deliverToWorker(ctx, env, content)
	}
	committed := false
	defer func() {
		if !committed {
			lease.Abort()
		}
	}()
	deliverAsNewTurn := func() (bool, error) {
		err := h.deliverToWorkerWithBusyHandling(ctx, env, content, false)
		if errors.Is(err, execution.ErrSessionBusy) {
			return false, nil
		}
		if err == nil {
			lease.Commit(supplementNormal)
			committed = true
		}
		return true, err
	}
	if w := h.sm.GetWorker(env.SessionID); w != nil {
		if inj, ok := w.(worker.MidTurnInjector); ok && !w.IsStopped() {
			// Re-check the active gate immediately before injecting. A race
			// window exists between the SESSION_BUSY detection (in
			// acceptInputExecutionWithRetry above) and this inject: if the
			// running turn's Done arrived in between and released the gate,
			// injecting now would write the supplement as the PRIMARY input of
			// a new unsolicited CC turn — CC headless stays alive reading stdin
			// after Done — producing a ghost turn with no execution record. If
			// the gate is already released, route to the normal delivery path so
			// the supplement becomes a proper new turn. Codex avoids this via
			// SteerTurn's expectedTurnID; Claude Code uses a worker-local active-turn
			// fence so InjectMidTurn rejects writes after Done even if the gate
			// changes between this check and the stdin write.
			if h.executionStore != nil {
				if _, aerr := h.executionStore.ActiveBySession(ctx, env.SessionID); errors.Is(aerr, execution.ErrNotFound) {
					if handled, deliverErr := deliverAsNewTurn(); handled {
						return deliverErr
					}
				}
			}
			if err := inj.InjectMidTurn(ctx, content, nil); err == nil {
				observability.MidTurnInjected().Add(ctx, 1)
				if h.bridge != nil {
					h.bridge.CaptureInboundEvent(env.SessionID, env.Seq, events.Input, env.Event.Data)
				}
				lease.Commit(supplementInjected)
				committed = true
				h.ackSupplement(ctx, env, "injected", false)
				h.notifySupplement(ctx, env.SessionID, "injected")
				return nil
			} else {
				h.log.Warn("gateway: mid-turn inject failed, falling back to buffer",
					"session_id", env.SessionID, "err", err)
				if h.executionStore != nil {
					if _, aerr := h.executionStore.ActiveBySession(ctx, env.SessionID); errors.Is(aerr, execution.ErrNotFound) {
						if handled, deliverErr := deliverAsNewTurn(); handled {
							return deliverErr
						}
					}
				}
			}
		}
	}
	if !h.bridge.BufferPending(env.SessionID, env, content) {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "supplement buffer is full")
	}
	lease.Commit(supplementBuffered)
	committed = true
	observability.SupplementBuffered().Add(ctx, 1)
	h.ackSupplement(ctx, env, "buffered", false)
	h.notifySupplement(ctx, env.SessionID, "buffered")
	return nil
}

// ackSupplement settles AEP clients for a busy input that was accepted outside
// the execution ledger. The stable synthetic execution ID mirrors command ACKs;
// client_message_id remains the authoritative correlation and dedup key.
func (h *Handler) ackSupplement(ctx context.Context, source *events.Envelope, mode string, duplicate bool) {
	if h.hub == nil {
		return
	}
	clientID := clientMessageID(source)
	ack := events.NewEnvelope(aep.NewID(), source.SessionID, h.hub.NextSeq(source.SessionID), events.InputAck, events.InputAckData{
		ClientMessageID: clientID,
		ExecutionID:     "supplement-" + clientID,
		Status:          events.ExecutionStatusDelivered,
		Duplicate:       duplicate,
	})
	ack.Priority = events.PriorityControl
	ack.OwnerID = source.OwnerID
	ack.Metadata = map[string]any{"client_message_id": clientID}
	if mode != "" {
		ack.Metadata["supplement_mode"] = mode
	}
	if err := h.hub.SendToSession(context.WithoutCancel(ctx), ack); err != nil {
		h.log.Warn("gateway: supplement ack delivery failed", "err", err,
			"session_id", source.SessionID, "client_message_id", clientID)
	}
}

// notifySupplement broadcasts a marker `message` envelope so platform conns
// (Slack/Feishu/Webchat) can render their own i18n "supplement accepted"
// text. Metadata["supplement_mode"] carries the mode ("injected"|"buffered");
// Content is empty — conns substitute their own localized text (Task 10).
// Best-effort: delivery failures are not surfaced to the user.
func (h *Handler) notifySupplement(ctx context.Context, sessionID, mode string) {
	env := events.NewEnvelope(aep.NewID(), sessionID, 0,
		events.Message, events.MessageData{Content: ""})
	env.Metadata = map[string]any{"supplement_mode": mode}
	_ = h.hub.SendToSession(ctx, env)
}

// DeliverReplay replays a buffered supplement as a fresh input turn.
// Implements bridge.PendingReplayer; the active gate is already released by
// the prior done, so deliverToWorker's accept path will succeed. The content
// is extracted from the envelope's Data map (preserved by cloneForReplay).
func (h *Handler) DeliverReplay(ctx context.Context, env *events.Envelope) error {
	data, _ := env.Event.Data.(map[string]any)
	content, _ := data["content"].(string)
	// The Bridge already holds the session replay read fence. Returning a raw
	// busy result lets replayPending requeue without recursively entering the
	// supplement handler and acquiring the same RWMutex behind a queued writer.
	return h.deliverToWorkerWithBusyHandling(ctx, env, content, false)
}

// deliverToWorker validates session state, handles IDLE→RUNNING transition,
// and delivers user input to the worker process.
func (h *Handler) deliverToWorker(ctx context.Context, env *events.Envelope, content string) error {
	return h.deliverToWorkerWithBusyHandling(ctx, env, content, true)
}

func (h *Handler) deliverToWorkerWithBusyHandling(ctx context.Context, env *events.Envelope, content string, handleBusy bool) error {
	inputReceivedAt := time.Now()
	_, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		h.log.Warn("gateway: handleInput session not found", "session_id", env.SessionID, "err", err)
		h.emitAudit(audit.OutcomeFailure, env.OwnerID, "", env.SessionID, content)
		h.cancelRetryIfNeeded(env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	execRecord, duplicate, err := h.acceptInputExecutionWithRetry(ctx, env)
	if err != nil {
		if errors.Is(err, execution.ErrPayloadConflict) {
			observability.ExecutionConflict().Add(ctx, 1)
			return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage,
				"client message id %q was already used with different input", clientMessageID(env))
		}
		if errors.Is(err, execution.ErrSessionBusy) {
			observability.ExecutionSessionBusy().Add(ctx, 1)
			h.cancelRetryIfNeeded(env.SessionID)
			if !handleBusy {
				return execution.ErrSessionBusy
			}
			return h.handleSupplementOnBusy(ctx, env, content)
		}
		h.log.Error("gateway: persist input acceptance failed", "err", err, "session_id", env.SessionID)
		// A genuine new input failed to durably register; cancel any in-flight
		// LLM retry so it cannot fire and answer the previous failed turn.
		h.cancelRetryIfNeeded(env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "input acceptance failed")
	}
	if duplicate {
		observability.ExecutionDuplicate().Add(ctx, 1)
		h.log.Info("gateway: duplicate input suppressed",
			"session_id", env.SessionID,
			"client_message_id", execRecord.ClientMessageID,
			observability.KeyExecutionID, execRecord.ExecutionID,
			"status", execRecord.Status)
		h.sendInputAck(ctx, env, execRecord, true)
		return nil
	}
	finalized := execRecord == nil
	if !finalized {
		observability.ExecutionAccept().Add(ctx, 1)
		if h.bridge != nil {
			h.bridge.beginTurnTTFT(env.SessionID, execRecord.ExecutionID, worker.WorkerType("unknown"), inputReceivedAt)
			h.bridge.markTurnDurablyAccepted(env.SessionID, execRecord.ExecutionID)
		}
	}
	defer func() {
		if finalized {
			return
		}
		if err := h.finishInputExecution(ctx, execRecord, execution.StatusUnknown, events.ErrCodeInternalError); err != nil {
			h.log.Error("gateway: persist abandoned input status failed", "err", err,
				"session_id", env.SessionID, observability.KeyExecutionID, execRecord.ExecutionID)
		}
	}()
	// The first acknowledgement means the input is durably recorded. A second
	// acknowledgement below reports the worker-delivery outcome.
	h.sendInputAck(ctx, env, execRecord, false)
	h.cancelRetryIfNeeded(env.SessionID)

	finishOutcome := func(status execution.Status, code events.ErrorCode) {
		if statusErr := h.finishInputExecution(ctx, execRecord, status, code); statusErr != nil {
			h.log.Error("gateway: persist input outcome failed", "err", statusErr,
				"session_id", env.SessionID, observability.KeyExecutionID, execRecord.ExecutionID, "status", status)
		}
		// The outcome is recorded on the in-memory record (and reflected in the
		// ack) regardless of durable-write success; gateway-restart recovery
		// reconciles the DB. finalized=true ensures the defer safety-net does
		// not overwrite the intended outcome with a generic unknown.
		finalized = true
		h.sendInputAck(ctx, env, execRecord, false)
		if h.bridge != nil && status == execution.StatusFailed {
			h.bridge.finishTurnTTFT(env.SessionID, string(status))
		}
	}
	// Fence recovery may have replaced the Worker and transitioned a TERMINATED
	// session back to RUNNING while accepting this input. Re-read state so the
	// fresh Worker is not immediately replaced by the normal resume path below.
	si, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		h.emitAudit(audit.OutcomeFailure, env.OwnerID, "", env.SessionID, content)
		finishOutcome(execution.StatusFailed, events.ErrCodeSessionNotFound)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found after input acceptance")
	}

	if !si.State.IsActive() {
		// Auto-resume TERMINATED sessions so the user can continue on the
		// same WebSocket connection after clicking stop (which sends terminate).
		if si.State == events.StateTerminated && h.bridge != nil {
			h.log.Info("gateway: auto-resuming terminated session", "session_id", env.SessionID)
			resumeCtx, resumeCancel := context.WithTimeout(ctx, 30*time.Second)
			resumeErr := h.bridge.ResumeSession(resumeCtx, env.SessionID, si.WorkDir)
			resumeCancel()
			if resumeErr != nil {
				h.log.Warn("gateway: auto-resume failed", "session_id", env.SessionID, "err", resumeErr)
				h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
				finishOutcome(execution.StatusFailed, events.ErrCodeInternalError)
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "session resume failed: %v", resumeErr)
			}
			prevPlatform := si.Platform
			si, err = h.sm.Get(ctx, env.SessionID)
			if err != nil {
				h.emitAudit(audit.OutcomeFailure, env.OwnerID, prevPlatform, env.SessionID, content)
				finishOutcome(execution.StatusFailed, events.ErrCodeSessionNotFound)
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found after resume")
			}
			if !si.State.IsActive() {
				h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
				finishOutcome(execution.StatusFailed, events.ErrCodeSessionBusy)
				return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session not active after resume: %s", si.State)
			}
		} else {
			h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
			finishOutcome(execution.StatusFailed, events.ErrCodeSessionBusy)
			return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session not active: %s", si.State)
		}
	}

	var w worker.Worker
	workerRunID := ""
	if h.bridge != nil {
		var ok bool
		w, workerRunID, ok = h.bridge.CurrentWorkerBinding(env.SessionID)
		if !ok && si.State.IsActive() && h.bridge.sm != nil {
			h.log.Info("gateway: auto-resuming active session lacking worker binding", "session_id", env.SessionID, "state", si.State)
			resumeCtx, resumeCancel := context.WithTimeout(ctx, 30*time.Second)
			resumeErr := h.bridge.ResumeSession(resumeCtx, env.SessionID, si.WorkDir)
			resumeCancel()
			if resumeErr != nil {
				h.log.Warn("gateway: auto-resume failed for active session lacking worker binding", "session_id", env.SessionID, "err", resumeErr)
				h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
				finishOutcome(execution.StatusFailed, events.ErrCodeInternalError)
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "session resume failed: %v", resumeErr)
			}
			// Refresh session info after successful resume
			var getErr error
			si, getErr = h.sm.Get(ctx, env.SessionID)
			if getErr != nil {
				h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
				finishOutcome(execution.StatusFailed, events.ErrCodeSessionNotFound)
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found after resume")
			}
			w, workerRunID, ok = h.bridge.CurrentWorkerBinding(env.SessionID)
		}
		if !ok {
			if h.executionStore != nil {
				finishOutcome(execution.StatusFailed, events.ErrCodeInternalError)
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker run identity unavailable")
			}
			w = h.sm.GetWorker(env.SessionID)
		}
	} else {
		w = h.sm.GetWorker(env.SessionID)
	}
	if w == nil {
		h.log.Warn("gateway: handleInput no worker found", "session_id", env.SessionID)
		h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
		finishOutcome(execution.StatusFailed, events.ErrCodeSessionNotFound)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "no worker attached to session")
	}
	if h.bridge != nil && execRecord != nil {
		h.bridge.setTurnTTFTWorkerType(env.SessionID, execRecord.ExecutionID, w.Type())
	}

	if si.State == events.StateIdle {
		if err := h.sm.TransitionWithInput(ctx, env.SessionID, events.StateRunning, content, nil); err != nil {
			h.log.Warn("gateway: handleInput transition failed", "session_id", env.SessionID, "err", err)
			h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
			finishOutcome(execution.StatusFailed, events.ErrCodeSessionBusy)
			return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session busy: %v", err)
		}
	}

	if execRecord != nil && h.executionStore != nil {
		persistedRunID := execRecord.WorkerRunID
		if workerRunID == "" {
			workerRunID = persistedRunID
		}
		if mrErr := h.executionStore.MarkRunning(ctx, execRecord.ExecutionID, h.ownerInstanceID, workerRunID); mrErr != nil {
			h.log.Warn("gateway: mark execution running failed", "err", mrErr,
				"session_id", env.SessionID, observability.KeyExecutionID, execRecord.ExecutionID)
			h.finishRuntimeWithoutDispatch(ctx, execRecord, persistedRunID, events.ErrCodeInternalError)
			finishOutcome(execution.StatusFailed, events.ErrCodeInternalError)
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "execution dispatch registration failed")
		}
		execRecord.WorkerRunID = workerRunID
	}

	if h.log.Enabled(ctx, slog.LevelDebug) {
		runes := []rune(content)
		preview := string(runes)
		if len(runes) > 32 {
			preview = string(runes[:32]) + "..."
		}
		h.log.Debug("gateway: delivering input to worker", "session_id", env.SessionID, "content_len", len(content), "preview", preview)
	}

	// Stamp the turn start immediately before delivery so the Done-bound timer
	// measures only this turn's processing, excluding inter-turn idle
	// (Turn-Integrity spec RC-4 / Fix D).
	if h.bridge != nil {
		h.bridge.RecordTurnStart(env.SessionID)
	}

	// A new primary turn begins: clear the previous turn's stop claim so this
	// turn can be stopped again (per-turn single-stop contract, C04/C05). The
	// fence is same-session/same-run/same-execution scoped; a new turn carries
	// a NEW execution ID, so this clear only matches when the execution ledger
	// is disabled (empty execID) — the exec-scoped key keeps the in-flight
	// stop's claim intact when the input path races the stop path. A replaced
	// worker run or execution is cleared by its own Claim overwriting the
	// stale entry. Metadata responses and mid-turn injections do NOT reach
	// this point and keep the claim.
	execID := ""
	if execRecord != nil {
		execID = execRecord.ExecutionID
	}
	h.stopFence.BeginTurn(env.SessionID, workerRunID, execID)

	if err := w.Input(ctx, content, nil); err != nil {
		var we *worker.WorkerError
		if errors.As(err, &we) && we.Kind == worker.ErrKindTimeout {
			h.log.Info("gateway: worker input delivery timed out (worker still processing)", "session_id", env.SessionID)
			finishOutcome(execution.StatusUnknown, events.ErrCodeExecutionTimeout)
			return nil
		}
		h.log.Warn("gateway: worker input", "err", err, "session_id", env.SessionID)
		// Input never reached the worker: clear the turn start so a later Done
		// (or crash-cleanup) cannot bill idle time to a turn that never ran.
		if h.bridge != nil {
			h.bridge.ClearTurnStart(env.SessionID)
		}
		code := classifyWorkerError(err)
		finishOutcome(execution.StatusFailed, code)
		// ErrKindUnavailable (e.g. ACP session lost) means the worker's
		// internal session is dead but the process may still be alive.
		// Send SESSION_TERMINATED so the client can reconnect, and trigger
		// crash cleanup so forwardEvents exits and the worker is replaced.
		if code == events.ErrCodeSessionTerminated && h.bridge != nil {
			h.bridge.cleanupCrashedWorker(env.SessionID, w)
		}
		h.emitAudit(audit.OutcomeFailure, env.OwnerID, si.Platform, env.SessionID, content)
		return h.sendErrorf(ctx, env, code, "worker input failed: %v", err)
	}
	if h.bridge != nil && execRecord != nil {
		h.bridge.markTurnWorkerAccepted(env.SessionID, execRecord.ExecutionID)
	}
	if err := h.finishInputExecution(ctx, execRecord, execution.StatusDelivered, ""); err != nil {
		h.log.Error("gateway: persist delivered input status failed", "err", err,
			"session_id", env.SessionID, observability.KeyExecutionID, execRecord.ExecutionID)
		// The worker accepted the input but the durable status write failed —
		// treat the outcome as ambiguous for the client.
		execRecord.Status = execution.StatusUnknown
		execRecord.ErrorCode = string(events.ErrCodeInternalError)
		finalized = true
		h.sendInputAck(ctx, env, execRecord, false)
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "input delivery status is unknown")
	}
	finalized = true
	h.sendInputAck(ctx, env, execRecord, false)
	h.log.Debug("gateway: input delivered to worker", "session_id", env.SessionID)
	h.emitAudit(audit.OutcomeSuccess, env.OwnerID, si.Platform, env.SessionID, content)
	if h.bridge != nil {
		h.bridge.CaptureInbound(ctx, env.SessionID, env.Seq, events.Input, env.Event.Data, si.Platform, si.OwnerID)
	}
	return nil
}

func (h *Handler) acceptInputExecution(ctx context.Context, env *events.Envelope) (*execution.Record, bool, error) {
	if h.executionStore == nil {
		return nil, false, nil
	}
	payloadHash, err := inputPayloadHash(env)
	if err != nil {
		return nil, false, err
	}
	workerRunID := ""
	if h.bridge != nil {
		workerRunID, _ = h.bridge.CurrentWorkerRunID(env.SessionID)
	}
	if workerRunID == "" {
		// Duplicate lookup must remain possible even when the session currently has
		// no Worker. A newly accepted record is rebound to the actual attach run by
		// MarkRunning after resume/fresh-start readiness.
		workerRunID = "run_" + uuid.NewString()
	}
	record, duplicate, err := h.executionStore.Accept(ctx, execution.AcceptRequest{
		SessionID:       env.SessionID,
		ClientMessageID: clientMessageID(env),
		PayloadHash:     payloadHash,
		OwnerInstanceID: h.ownerInstanceID,
		WorkerRunID:     workerRunID,
	})
	if err != nil {
		return nil, duplicate, err
	}
	return record, duplicate, nil
}

func inputPayloadHash(env *events.Envelope) (string, error) {
	// Hash only the user payload (content + metadata). Transport metadata such
	// as platform_msg_id is also the idempotency key, so including it would
	// couple the lookup key to the hashed payload — strip it before hashing.
	hashData := env.Event.Data
	if data, ok := hashData.(map[string]any); ok {
		if _, has := data["platform_msg_id"]; has {
			cleaned := make(map[string]any, len(data))
			for k, v := range data {
				if k == "platform_msg_id" {
					continue
				}
				cleaned[k] = v
			}
			hashData = cleaned
		}
	}
	payload, err := json.Marshal(hashData)
	if err != nil {
		return "", fmt.Errorf("marshal input for idempotency: %w", err)
	}
	return sha256Hex(string(payload)), nil
}

// acceptInputExecutionWithRetry accepts the input, and if the session is fenced
// (previous runtime outcome unknown, which blocks Accept via the active gate's
// partial unique index), clears the fence and retries once. Without this a
// fenced session stays permanently blocked. Worker health is left to the
// existing session/crash-recovery machinery, so clearing the fence only
// re-opens Accept; the old record stays runtime_status=unknown in history.
func (h *Handler) acceptInputExecutionWithRetry(ctx context.Context, env *events.Envelope) (*execution.Record, bool, error) {
	rec, duplicate, err := h.acceptInputExecution(ctx, env)
	if err == nil || !errors.Is(err, execution.ErrSessionBusy) || h.executionStore == nil {
		return rec, duplicate, err
	}
	fenced, ferr := h.executionStore.FenceBySession(ctx, env.SessionID)
	if ferr != nil || fenced == nil {
		return rec, duplicate, err
	}
	if h.bridge == nil {
		return rec, duplicate, err
	}
	freshRunID, startErr := h.bridge.StartFreshWorker(ctx, env.SessionID)
	if startErr != nil {
		h.log.Warn("gateway: fresh worker start for fenced execution failed",
			"err", startErr, "session_id", env.SessionID, observability.KeyExecutionID, fenced.ExecutionID)
		return rec, duplicate, err
	}
	if cerr := h.executionStore.ClearFenceAfterFreshStart(ctx, fenced.ExecutionID, fenced.FenceReason, freshRunID); cerr != nil {
		h.log.Warn("gateway: clear stale fence failed",
			"err", cerr, "session_id", env.SessionID, observability.KeyExecutionID, fenced.ExecutionID)
		return rec, duplicate, err
	}
	h.log.Info("gateway: cleared stale fence, retrying accept",
		"session_id", env.SessionID, observability.KeyExecutionID, fenced.ExecutionID,
		"fence_reason", fenced.FenceReason)
	return h.acceptInputExecution(ctx, env)
}

func clientMessageID(env *events.Envelope) string {
	if data, ok := env.Event.Data.(map[string]any); ok {
		if id, _ := data["client_message_id"].(string); id != "" {
			return id
		}
		if id, _ := data["platform_msg_id"].(string); id != "" {
			return id
		}
	}
	if env.ID != "" {
		return env.ID
	}
	return aep.NewID()
}

// ackControlCommand sends a synthetic "delivered" InputAck for an input that
// was intercepted as a control/help/worker command instead of being routed to
// the durable execution pipeline. The webchat client arms a pending-input lock
// on every sendInput that only clears on a terminal InputAck/Done/Error; without
// this ack the UI locks for the full settle timeout (5 min) after commands like
// /reset that emit only a State event.
func (h *Handler) ackControlCommand(ctx context.Context, env *events.Envelope) {
	if h.hub == nil {
		return
	}
	ack := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.InputAck, events.InputAckData{
		ClientMessageID: clientMessageID(env),
		ExecutionID:     "cmd-" + env.ID,
		Status:          events.ExecutionStatusDelivered,
	})
	ack.Priority = events.PriorityControl
	ack.OwnerID = env.OwnerID
	if err := h.hub.SendToSession(context.WithoutCancel(ctx), ack); err != nil {
		h.log.Warn("gateway: control command ack failed", "err", err, "session_id", env.SessionID)
	}
}

func (h *Handler) finishInputExecution(ctx context.Context, record *execution.Record, status execution.Status, code events.ErrorCode) error {
	if h.executionStore == nil || record == nil {
		return nil
	}
	// Reflect the intended terminal status on the in-memory record before the
	// durable write, so every subsequent input.ack carries the outcome the
	// client should act on even when SetDelivery fails. The defer safety-net and
	// gateway-restart recovery reconcile the durable record afterwards.
	record.Status = status
	record.ErrorCode = string(code)
	if err := h.executionStore.SetDelivery(context.WithoutCancel(ctx), record.ExecutionID, h.ownerInstanceID, status, string(code)); err != nil {
		if h.repairer != nil {
			h.repairer.Enqueue(execution.RepairIntent{
				ExecutionID: record.ExecutionID,
				OwnerID:     h.ownerInstanceID,
				Kind:        execution.RepairDelivery,
				Status:      string(status),
				ErrorCode:   string(code),
			})
		}
		return err
	}
	observability.ExecutionDeliveryOutcome().Add(ctx, 1,
		metric.WithAttributes(attribute.String("delivery_status", string(status))))
	if record.CreatedAt > 0 {
		observability.ExecutionDeliveryLatency().Record(ctx, time.Since(time.UnixMilli(record.CreatedAt)).Seconds())
	}
	return nil
}

func (h *Handler) finishRuntimeWithoutDispatch(ctx context.Context, record *execution.Record, workerRunID string, code events.ErrorCode) {
	if h.executionStore == nil || record == nil || workerRunID == "" {
		return
	}
	err := h.executionStore.FinishRuntime(
		context.WithoutCancel(ctx), record.ExecutionID, workerRunID, execution.RuntimeFailed, string(code),
	)
	if err == nil || h.repairer == nil {
		return
	}
	h.repairer.Enqueue(execution.RepairIntent{
		ExecutionID: record.ExecutionID,
		WorkerRunID: workerRunID,
		Kind:        execution.RepairRuntime,
		Status:      string(execution.RuntimeFailed),
		ErrorCode:   string(code),
	})
}

func (h *Handler) finishRuntimeOnStop(ctx context.Context, sessionID, workerRunID, ownerID string) {
	if h.executionStore == nil {
		return
	}
	rec, err := h.executionStore.OpenBySession(ctx, sessionID)
	if err != nil {
		return
	}

	rtStatus := execution.RuntimeFailed
	eventKind := events.RuntimeExecutionFailed
	errorCode := string(events.ErrCodeSessionTerminated)

	err = h.executionStore.FinishRuntime(
		context.WithoutCancel(ctx), rec.ExecutionID, workerRunID, rtStatus, errorCode,
	)
	if err != nil {
		h.log.Warn("gateway: finish runtime on stop failed, enqueuing repair", "err", err, "session_id", sessionID, observability.KeyExecutionID, rec.ExecutionID)
		if h.repairer != nil {
			h.repairer.Enqueue(execution.RepairIntent{
				ExecutionID: rec.ExecutionID,
				WorkerRunID: workerRunID,
				Kind:        execution.RepairRuntime,
				Status:      string(rtStatus),
				ErrorCode:   errorCode,
			})
		}
	}

	rtEnv := events.NewEnvelope(aep.NewID(), sessionID, 0, eventKind, events.RuntimeExecutionData{
		ExecutionID: rec.ExecutionID,
		Status:      string(rtStatus),
		ErrorCode:   events.ErrorCode(errorCode),
	})
	rtEnv.OwnerID = ownerID
	_ = h.hub.SendToSession(ctx, rtEnv)
}

func (h *Handler) sendInputAck(ctx context.Context, source *events.Envelope, record *execution.Record, duplicate bool) {
	if h.hub == nil || record == nil {
		return
	}
	ack := events.NewEnvelope(aep.NewID(), source.SessionID, h.hub.NextSeq(source.SessionID), events.InputAck, events.InputAckData{
		ClientMessageID: record.ClientMessageID,
		ExecutionID:     record.ExecutionID,
		Status:          events.ExecutionStatus(record.Status),
		Duplicate:       duplicate,
		ErrorCode:       events.ErrorCode(record.ErrorCode),
	})
	ack.Priority = events.PriorityControl
	ack.OwnerID = source.OwnerID
	ack.Metadata = map[string]any{
		observability.KeyExecutionID: record.ExecutionID,
		"client_message_id":          record.ClientMessageID,
	}
	if err := h.hub.SendToSession(context.WithoutCancel(ctx), ack); err != nil {
		h.log.Warn("gateway: input ack delivery failed", "err", err,
			"session_id", source.SessionID, observability.KeyExecutionID, record.ExecutionID)
	}
}

func (h *Handler) handlePing(ctx context.Context, env *events.Envelope) error {
	// Include current session state in pong (per AEP spec §11.4).
	si, err := h.sm.Get(ctx, env.SessionID)
	state := "unknown"
	if err == nil {
		state = string(si.State)
	}

	reply := events.NewEnvelope(
		aep.NewID(),
		env.SessionID,
		0, // P2: pong should not consume seq
		events.Pong,
		map[string]any{"state": state},
	)
	if h.log.Enabled(ctx, LevelTrace) {
		h.log.Log(ctx, LevelTrace, "gateway: ping received, sending pong", "session_id", env.SessionID, "state", state)
	}
	err = h.hub.SendToSession(ctx, reply)
	if err != nil {
		h.log.Warn("gateway: pong send failed", "session_id", env.SessionID, "err", err)
	}
	return err
}

var passthroughMetricLabel = map[events.Kind]string{
	events.Reasoning:           "reasoning",
	events.Step:                "step",
	events.PermissionRequest:   "permission_request",
	events.PermissionResponse:  "permission_response",
	events.QuestionRequest:     "question_request",
	events.QuestionResponse:    "question_response",
	events.ElicitationRequest:  "elicitation_request",
	events.ElicitationResponse: "elicitation_response",
	events.Message:             "message",
	events.MessageStart:        "message.start",
	events.MessageEnd:          "message.end",
}

func (h *Handler) passthroughToSession(ctx context.Context, env *events.Envelope) error {
	if label, ok := passthroughMetricLabel[env.Event.Type]; ok {
		observability.GatewayEvents().Add(ctx, 1, metric.WithAttributes(attribute.String("event_type", label), attribute.String("direction", "s2c")))
	}
	return h.hub.SendToSession(ctx, env)
}

// validateOwner checks ownership and returns the session in one call.
// This avoids the double-fetch that calling ValidateOwnership then Get separately incurses.
func (h *Handler) validateOwner(ctx context.Context, env *events.Envelope) (*session.SessionInfo, error) {
	si, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		return nil, err
	}
	if si.UserID != env.OwnerID {
		return nil, fmt.Errorf("%w: owner mismatch", session.ErrOwnershipMismatch)
	}
	return si, nil
}

// requireActiveOwner validates session ownership and returns the session info.
// On error it sends an appropriate error to the client and returns the error
// so the caller can simply do: si, err := h.requireActiveOwner(ctx, env); if err != nil { return err }
func (h *Handler) requireActiveOwner(ctx context.Context, env *events.Envelope) (*session.SessionInfo, error) {
	si, err := h.validateOwner(ctx, env)
	if err != nil {
		if errors.Is(err, session.ErrSessionCleanupPending) {
			return nil, h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session cleanup in progress; retry later")
		}
		if errors.Is(err, session.ErrSessionNotFound) {
			return nil, h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return nil, h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
	}
	return si, nil
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// SessionReader provides read-only session access.
type SessionReader interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
}

// SessionLifecycle provides session creation and deletion.
type SessionLifecycle interface {
	CreateWithBot(ctx context.Context, id, userID, botID, botName string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workspaceID, workDir, title, clientKey string) (*session.SessionInfo, error)
	Delete(ctx context.Context, id string) error
	DeletePhysical(ctx context.Context, id string) error
}

// SessionTransitioner provides state transition operations.
type SessionTransitioner interface {
	Transition(ctx context.Context, id string, to events.SessionState) error
	TransitionWithInput(ctx context.Context, id string, to events.SessionState, content string, metadata map[string]any) error
	TransitionWithReason(ctx context.Context, id string, to events.SessionState, termReason string) error
}

// SessionWorkerManager provides worker attachment and detachment.
type SessionWorkerManager interface {
	AttachWorker(ctx context.Context, id string, w worker.Worker) error
	DetachWorker(id string)
	DetachWorkerIf(id string, expected worker.Worker) bool
	UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error
	EnsureWorkerSessionID(ctx context.Context, id, workerSessionID string) error
	SetPermissionCeilingIfEmpty(ctx context.Context, id, ceiling string) (string, error)
}

// SessionAdmin provides listing, ownership validation, and metadata mutations.
type SessionAdmin interface {
	SessionExpirer
	List(ctx context.Context, userID, platform, workspaceID string, limit, offset int) ([]*session.SessionInfo, error)
	ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error
	UpdateWorkDir(ctx context.Context, id, workDir string) error
}

// SessionExpirer resets session expiry timers. Extracted as a single-method
// interface so bridgeSM can compose it alongside reader/lifecycle/transition
// sub-interfaces without pulling in the full SessionAdmin.
type SessionExpirer interface {
	ResetExpiry(ctx context.Context, id string) error
}

// SessionManager composes all session sub-interfaces for full management.
type SessionManager interface {
	SessionReader
	SessionLifecycle
	SessionTransitioner
	SessionWorkerManager
	SessionAdmin
}

// WorkerFactory creates worker instances. Production code uses defaultWorkerFactory.
type WorkerFactory interface {
	NewWorker(t worker.WorkerType) (worker.Worker, error)
}

type defaultWorkerFactory struct{}

func (defaultWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	return worker.NewWorker(t)
}

func (h *Handler) handleInteractionResponseEvent(ctx context.Context, env *events.Envelope) error {
	h.cancelRetryIfNeeded(env.SessionID)

	si, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		h.log.Warn("gateway: interaction response session not found", "session_id", env.SessionID, "err", err)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	w := h.sm.GetWorker(env.SessionID)
	if w == nil {
		h.log.Warn("gateway: interaction response no worker attached", "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "no worker attached to session")
	}

	metadata, err := interactionResponseMetadata(env.Event.Type, env.Event.Data)
	if err != nil {
		h.log.Warn("gateway: normalize interaction response failed", "err", err, "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "invalid response data: %v", err)
	}

	if err := w.Input(ctx, "", metadata); err != nil {
		h.log.Warn("gateway: worker interaction response delivery failed", "err", err, "session_id", env.SessionID)
		code := classifyWorkerError(err)
		if errors.Is(err, base.ErrInvalidSchema) {
			code = events.ErrCodeInvalidMessage
		}
		// Send error envelope but include request_id in Metadata to allow UI correlation
		var reqID string
		if dataMap, ok := env.Event.Data.(map[string]any); ok {
			reqID, _ = dataMap["id"].(string)
			if reqID == "" {
				reqID, _ = dataMap["request_id"].(string)
			}
		}
		errEnv := events.NewEnvelope(aep.NewID(), env.SessionID, 0, events.Error, events.ErrorData{
			Code:    code,
			Message: fmt.Sprintf("worker response failed: %v", err),
		})
		if reqID != "" {
			errEnv.Metadata = map[string]any{
				"interaction_error": map[string]any{
					"request_id": reqID,
				},
			}
		}
		_ = h.hub.SendToSession(ctx, errEnv)
		return fmt.Errorf("%s: worker response failed: %w", code, err)
	}

	h.emitInteractionAudit(env.OwnerID, si.Platform, env.SessionID, env.Event.Type, env.Event.Data, "")
	if h.bridge != nil {
		h.bridge.CaptureInboundEvent(env.SessionID, env.Seq, env.Event.Type, env.Event.Data)
	}
	// Explicit AEP interaction responses (used by WebChat) are acknowledged
	// only after the Worker native response endpoint accepts them. WebSocket
	// send success alone is not delivery success, so the browser treats this
	// correlated echo as the authoritative resolved/rejected transition.
	if h.hub != nil {
		ack := events.NewEnvelope(
			aep.NewID(),
			env.SessionID,
			0,
			env.Event.Type,
			env.Event.Data,
		)
		ack.OwnerID = env.OwnerID
		if err := h.hub.SendToSession(ctx, ack); err != nil {
			h.log.Warn("gateway: interaction response ack delivery failed",
				"err", err,
				"type", env.Event.Type,
				"session_id", env.SessionID)
		}
	}
	return nil
}

func interactionResponseMetadata(kind events.Kind, data any) (map[string]any, error) {
	dataMap, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response data must be a map")
	}

	metadata := make(map[string]any)
	switch kind {
	case events.PermissionResponse:
		id, _ := dataMap["id"].(string)
		if id == "" {
			id, _ = dataMap["request_id"].(string)
		}
		allowed, _ := dataMap["allowed"].(bool)
		reason, _ := dataMap["reason"].(string)

		metadata["permission_response"] = map[string]any{
			"id":         id,
			"request_id": id,
			"allowed":    allowed,
			"reason":     reason,
		}

	case events.QuestionResponse:
		id, _ := dataMap["id"].(string)
		answers := dataMap["answers"]
		metadata["question_response"] = map[string]any{
			"id":      id,
			"answers": answers,
		}

	case events.ElicitationResponse:
		id, _ := dataMap["id"].(string)
		action, _ := dataMap["action"].(string)
		content := dataMap["content"]
		metadata["elicitation_response"] = map[string]any{
			"id":      id,
			"action":  action,
			"content": content,
		}

	default:
		return nil, fmt.Errorf("unsupported interaction response kind: %s", kind)
	}

	return metadata, nil
}

// Compile-time assertion: *Handler satisfies bridge.PendingReplayer so the
// bridge can late-inject it via SetPendingReplayer for done-time replay.
var _ PendingReplayer = (*Handler)(nil)
