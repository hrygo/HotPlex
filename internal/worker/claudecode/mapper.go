package claudecode

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Mapper converts WorkerEvents to AEP envelopes.
type Mapper struct {
	log       *slog.Logger
	sessionID string
	seqGen    func() int64 // Sequence generator (provided by Hub)

	// sentTexts tracks the cumulative text/reasoning already sent for each item ID
	// to perform delta-based diff calculations. Sourced under mu.
	// Key shape: "<messageID>_<blockIndex>_<type>" (Turn-Integrity Fix B1).
	sentTexts map[string]string
	mu        sync.Mutex

	// turnEpoch scopes synthetic assistant IDs per turn so that messages without
	// a native id cannot collide across turns (Fix B2). Bumped on each Result;
	// sentTexts is cleared at the same boundary (Fix B3) to bound growth.
	turnEpoch  int64
	driftCount int64 // total snapshot identity divergences; diagnostic (Fix E)
}

// NewMapper creates a new Mapper instance.
func NewMapper(log *slog.Logger, sessionID string, seqGen func() int64) *Mapper {
	return &Mapper{
		log:       log,
		sessionID: sessionID,
		seqGen:    seqGen,
		sentTexts: make(map[string]string),
	}
}

// statusToSessionState maps Claude Code status strings to AEP session states.
// Returns ok=false for unknown status values.
func statusToSessionState(s string) (events.SessionState, bool) {
	switch s {
	case "idle":
		return events.StateIdle, true
	case "processing":
		return events.StateRunning, true
	default:
		return "", false
	}
}

// Map converts a WorkerEvent to one or more AEP envelopes.
// Returns nil slice for internal events that should not be sent to client.
func (m *Mapper) Map(evt *WorkerEvent) ([]*events.Envelope, error) {
	switch evt.Type {
	case EventStream:
		payload, ok := evt.Payload.(*StreamPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: stream payload is not *StreamPayload: %T", evt.Payload)
		}
		env, err := m.mapStream(payload)
		if err != nil {
			return nil, err
		}
		return []*events.Envelope{env}, nil
	case EventAssistant:
		switch p := evt.Payload.(type) {
		case *StreamPayload:
			env, err := m.mapStream(p)
			if err != nil {
				return nil, err
			}
			return []*events.Envelope{env}, nil
		case *ToolCallPayload:
			env, err := m.mapToolCall(p)
			if err != nil {
				return nil, err
			}
			return []*events.Envelope{env}, nil
		default:
			return nil, fmt.Errorf("mapper: unknown assistant payload type: %T", p)
		}
	case EventToolProgress:
		payload, ok := evt.Payload.(*ToolResultPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: tool_progress payload is not *ToolResultPayload: %T", evt.Payload)
		}
		env, err := m.mapToolProgress(payload)
		if err != nil {
			return nil, err
		}
		return []*events.Envelope{env}, nil
	case EventResult:
		payload, ok := evt.Payload.(*ResultPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: result payload is not *ResultPayload: %T", evt.Payload)
		}
		return m.mapResult(payload)
	case EventSystem, EventSessionState:
		raw, ok := evt.Payload.(json.RawMessage)
		if !ok {
			return nil, fmt.Errorf("mapper: %s payload is not json.RawMessage: %T", evt.Type, evt.Payload)
		}
		return m.mapRawStringPayload(raw, m.mapState)
	default:
		return nil, fmt.Errorf("mapper: unknown event type: %v", evt.Type)
	}
}

// mapRawStringPayload unmarshal a json.RawMessage payload to string and delegates
// to the given map function. Returns nil for non-string payloads (JSON objects).
func (m *Mapper) mapRawStringPayload(raw json.RawMessage, mapFn func(string) (*events.Envelope, error)) ([]*events.Envelope, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, nil
	}
	env, err := mapFn(s)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, nil
	}
	return []*events.Envelope{env}, nil
}

// mapStream converts a stream_event to an AEP envelope.
// thinking → events.Reasoning; all other types → events.MessageDelta.
func (m *Mapper) mapStream(p *StreamPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	m.mu.Lock()
	msgID := p.MessageID
	if msgID == "" {
		// No native message id: synthesize a turn-scoped identity instead of
		// falling back to a worker-lifetime constant. A constant key made
		// shorter follow-up turns silently empty (Turn-Integrity RC-2/Fix B2).
		msgID = fmt.Sprintf("assistant:%d:%d", m.turnEpoch, p.BlockIndex)
	}
	// Composite dedup key: native id + block index + content type. Multiple
	// content blocks in one message get isolated namespaces (Fix B1).
	dedupKey := fmt.Sprintf("%s_%d_%s", msgID, p.BlockIndex, p.Type)

	var content string
	if p.IsDelta {
		if m.sentTexts == nil {
			m.sentTexts = make(map[string]string)
		}
		m.sentTexts[dedupKey] += p.Content
		content = p.Content
	} else {
		content = m.diffSnapshotLocked(dedupKey, p.Content, msgID, p.Type)
	}
	m.mu.Unlock()

	if p.Type == "thinking" {
		return events.NewEnvelope(
			aep.NewID(),
			m.sessionID,
			m.seqGen(),
			events.Reasoning,
			events.ReasoningData{
				ID:      msgID,
				Content: content,
			},
		), nil
	}

	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.MessageDelta,
		events.MessageDeltaData{
			MessageID: msgID,
			Content:   content,
		},
	), nil
}

// mapToolCall converts tool_use to tool_call event.
func (m *Mapper) mapToolCall(p *ToolCallPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.ToolCall,
		events.ToolCallData{
			ID:    p.ID,
			Name:  p.Name,
			Input: p.Input,
		},
	), nil
}

// mapToolProgress converts tool_progress to tool_result.
func (m *Mapper) mapToolProgress(p *ToolResultPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.ToolResult,
		events.ToolResultData{
			ID:     p.ToolUseID,
			Output: p.Output,
			Error:  p.Error,
		},
	), nil
}

// mapResult converts result to done (+ optional error).
// Merges Usage and ModelUsage into DoneData.Stats so downstream consumers
// (Bridge accumulator, platform adapters) can extract token/cost/context data.
//
// Turn boundary (Turn-Integrity Fix B2/B3): on every Result the per-turn dedup
// state is cleared and turnEpoch bumped. This (a) prevents sentTexts from
// growing unboundedly across long sessions, and (b) ensures synthetic
// assistant IDs for the next turn cannot collide with this one. Native IDs
// already differ per message; the epoch only matters for the no-ID fallback.
func (m *Mapper) mapResult(p *ResultPayload) ([]*events.Envelope, error) {
	stats := make(map[string]any, len(p.Stats)+2)
	maps.Copy(stats, p.Stats)
	if p.Usage != nil {
		stats["usage"] = p.Usage
	}
	if p.ModelUsage != nil {
		stats["model_usage"] = p.ModelUsage
	}

	m.mu.Lock()
	m.sentTexts = make(map[string]string)
	m.turnEpoch++
	m.mu.Unlock()

	if !p.Success {
		msg := p.Message
		if msg == "" {
			msg = "worker execution failed"
		}
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), m.sessionID, m.seqGen(), events.Error, events.ErrorData{
				Code:    events.ErrCodeInternalError,
				Message: msg,
			}),
			events.NewEnvelope(aep.NewID(), m.sessionID, m.seqGen(), events.Done, events.DoneData{
				Success: false,
				Stats:   stats,
			}),
		}, nil
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), m.sessionID, m.seqGen(), events.Done, events.DoneData{
			Success: true,
			Stats:   stats,
		}),
	}, nil
}

// mapState converts system/session_state status to state event.
func (m *Mapper) mapState(status string) (*events.Envelope, error) {
	state, ok := statusToSessionState(status)
	if !ok {
		return nil, nil
	}

	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.State,
		events.StateData{
			State: state,
		},
	), nil
}

// MapControl maps a control-request event to AEP envelopes.
// All control-event AEP construction is consolidated here, removing inline
// json.Unmarshal calls and RawMessage access from the worker layer.
func (m *Mapper) MapControl(cr *ControlRequestPayload) []*events.Envelope {
	switch cr.Subtype {
	case string(ControlCanUseTool):
		if cr.ToolName == "AskUserQuestion" {
			return []*events.Envelope{m.mapQuestionRequest(cr)}
		}
		return []*events.Envelope{m.mapPermissionRequest(cr)}
	case "elicitation":
		return []*events.Envelope{m.mapElicitationRequest(cr)}
	default:
		return nil
	}
}

// mapQuestionRequest builds a QuestionRequest envelope from AskUserQuestion control.
func (m *Mapper) mapQuestionRequest(cr *ControlRequestPayload) *events.Envelope {
	var questions []events.Question
	if len(cr.Input) > 0 {
		var input struct {
			Questions []events.Question `json:"questions"`
		}
		if err := json.Unmarshal(cr.Input, &input); err != nil {
			m.log.Warn("mapper: question unmarshal failed", "err", err)
		}
		questions = input.Questions
	}
	return events.NewEnvelope(
		aep.NewID(), m.sessionID, m.seqGen(),
		events.QuestionRequest,
		events.QuestionRequestData{
			ID:        cr.RequestID,
			ToolName:  cr.ToolName,
			Questions: questions,
		},
	)
}

// mapPermissionRequest builds a PermissionRequest envelope for tool approval.
func (m *Mapper) mapPermissionRequest(cr *ControlRequestPayload) *events.Envelope {
	var input map[string]any
	if len(cr.Input) > 0 {
		if err := json.Unmarshal(cr.Input, &input); err != nil {
			m.log.Warn("mapper: permission input unmarshal failed", "err", err)
		}
	}
	args := []string{"{}"}
	if len(input) > 0 {
		if s, err := json.Marshal(input); err == nil {
			args = []string{string(s)}
		}
	}
	return events.NewEnvelope(
		aep.NewID(), m.sessionID, m.seqGen(),
		events.PermissionRequest,
		events.PermissionRequestData{
			ID:          cr.RequestID,
			ToolName:    cr.ToolName,
			Description: cr.ToolName,
			Args:        args,
			InputRaw:    cr.Input,
		},
	)
}

// mapElicitationRequest builds an ElicitationRequest envelope.
// Uses pre-parsed Elicitation fields from ControlRequestPayload (parsed in parser).
func (m *Mapper) mapElicitationRequest(cr *ControlRequestPayload) *events.Envelope {
	data := events.ElicitationRequestData{ID: cr.RequestID}
	if cr.Elicitation != nil {
		data.MCPServerName = cr.Elicitation.MCPServerName
		data.Message = cr.Elicitation.Message
		data.Mode = cr.Elicitation.Mode
		data.URL = cr.Elicitation.URL
		data.ElicitationID = cr.Elicitation.ElicitationID
		data.RequestedSchema = cr.Elicitation.RequestedSchema
	}
	return events.NewEnvelope(
		aep.NewID(), m.sessionID, m.seqGen(),
		events.ElicitationRequest, data,
	)
}

func mapContextUsageResponse(raw map[string]any) *events.ContextUsageData {
	return events.MapContextUsageResponse(raw)
}

func mapMCPStatusResponse(raw map[string]any) *events.MCPStatusData {
	return events.MapMCPStatusResponse(raw)
}

// diffSnapshotLocked computes the newly appended characters for a full
// cumulative snapshot against the previously recorded text. It must be called
// with m.mu held.
//
// Three cases (Turn-Integrity Fix B3):
//  1. Identical to what was sent → return "" (legal repeat, not a lost update).
//  2. Current is a strict extension of sent → return the tail and advance.
//  3. Prefix drift (shorter or divergent) → MUST NOT silently return "". Emit
//     the full snapshot under a fresh synthetic block identity and record an
//     integrity warning. This replaces the old length-only comparison that
//     dropped shorter follow-up turns entirely.
func (m *Mapper) diffSnapshotLocked(dedupKey, currentText, msgID, contentType string) string {
	if m.sentTexts == nil {
		m.sentTexts = make(map[string]string)
	}
	sent := m.sentTexts[dedupKey]
	switch {
	case currentText == sent:
		return ""
	case strings.HasPrefix(currentText, sent):
		delta := currentText[len(sent):]
		m.sentTexts[dedupKey] = currentText
		return delta
	default:
		// Prefix drift: record the full snapshot under a divergence-scoped key
		// so it is delivered in full rather than swallowed.
		driftKey := fmt.Sprintf("%s_drift_%d", dedupKey, m.driftCount)
		m.driftCount++
		m.sentTexts[driftKey] = currentText
		m.log.Warn("mapper: assistant snapshot identity divergence, emitting full snapshot",
			"message_id", msgID, "content_type", contentType,
			"sent_len", len(sent), "snapshot_len", len(currentText))
		return currentText
	}
}
