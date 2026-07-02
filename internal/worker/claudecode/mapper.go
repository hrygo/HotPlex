package claudecode

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Mapper converts WorkerEvents to AEP envelopes.
type Mapper struct {
	log       *slog.Logger
	sessionID string
	seqGen    func() int64 // Sequence generator (provided by Hub)

	// sentLengths tracks the length of text/reasoning already sent for each item ID
	// to perform delta-based diff calculations. Sourced under mu.
	sentLengths map[string]int
	mu          sync.Mutex
}

// NewMapper creates a new Mapper instance.
func NewMapper(log *slog.Logger, sessionID string, seqGen func() int64) *Mapper {
	return &Mapper{
		log:         log,
		sessionID:   sessionID,
		seqGen:      seqGen,
		sentLengths: make(map[string]int),
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
	msgID := p.MessageID
	if msgID == "" {
		msgID = "assistant_msg"
	}

	var content string
	if p.IsDelta {
		m.recordSentLength(msgID+"_"+p.Type, len(p.Content))
		content = p.Content
	} else {
		content = m.getDeltaText(msgID+"_"+p.Type, p.Content)
	}

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
func (m *Mapper) mapResult(p *ResultPayload) ([]*events.Envelope, error) {
	stats := make(map[string]any, len(p.Stats)+2)
	maps.Copy(stats, p.Stats)
	if p.Usage != nil {
		stats["usage"] = p.Usage
	}
	if p.ModelUsage != nil {
		stats["model_usage"] = p.ModelUsage
	}

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

// getDeltaText computes the newly appended characters for a given item ID
// by comparing the current text against the previously recorded sent length.
// It updates the recorded length to reflect the new state.
// Access to m.sentLengths is guarded under m.mu.
func (m *Mapper) getDeltaText(itemID, currentText string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sentLengths == nil {
		m.sentLengths = make(map[string]int)
	}

	lastLen := m.sentLengths[itemID]
	currLen := len(currentText)

	if currLen <= lastLen {
		return ""
	}

	delta := currentText[lastLen:]
	m.sentLengths[itemID] = currLen
	return delta
}

// recordSentLength updates the recorded sent length for a given item ID
// after delta content has been sent via another event source (like delta notification).
func (m *Mapper) recordSentLength(itemID string, deltaLength int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sentLengths == nil {
		m.sentLengths = make(map[string]int)
	}

	m.sentLengths[itemID] += deltaLength
}
