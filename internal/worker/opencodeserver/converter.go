package opencodeserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// OCS event type constants — used by Converter and singleton dispatch filter.
const (
	ocsStepStarted      = "session.next.step.started"
	ocsStepEnded        = "session.next.step.ended"
	ocsStepFailed       = "session.next.step.failed"
	ocsPartUpdated      = "message.part.updated" // OCS 1.17+: part metadata for text/reasoning deltas
	ocsPartDelta        = "message.part.delta"   // OCS 1.15+: unified text/reasoning delta
	ocsToolCalled       = "session.next.tool.called"
	ocsToolSuccess      = "session.next.tool.success"
	ocsToolFailed       = "session.next.tool.failed"
	ocsSessionStatus    = "session.status"
	ocsSessionIdle      = "session.idle"
	ocsSessionError     = "session.error"
	ocsReasoningStarted = "session.next.reasoning.started"
	ocsReasoningEnded   = "session.next.reasoning.ended"
	ocsPermAsked        = "permission.asked"
	ocsQuestionAsked    = "question.asked"
)

// Converter maps OCS BusEvents to AEP envelopes.
// It handles both V2 events (session.next.* prefix) and legacy V1 events
// (session.status, session.error, permission.asked, question.asked).
//
// Thread safety: Convert and Reset are NOT safe for concurrent use. They must
// only be called from the readGlobalSSE goroutine (which also calls
// dispatchToAllSubscribers). If future callers need concurrent access, add a
// mutex to Converter.
type Converter struct {
	states map[string]*turnState // sessionID → state
}

// turnState tracks per-session accumulation within a single turn.
// Reset when session.status(idle) fires or session.error occurs.
type turnState struct {
	cost            float64
	tokens          tokenAccum
	reasoningActive bool   // true between reasoning.started and reasoning.ended
	model           string // "providerID/modelID" from step.started
	doneShown       bool   // true once Done has been emitted for this turn (dedup)
	parts           map[string]partState
	// seenToolCalls dedups ToolCall per callID across the multi-mutation
	// message.part.updated lifecycle (pending → running → completed/error).
	seenToolCalls map[string]bool
}

type partState struct {
	typ       string
	messageID string
	ignored   bool
}

type tokenAccum struct {
	input, output, reasoning, cacheRead, cacheWrite int64
}

// NewConverter creates a Converter ready to use.
func NewConverter() *Converter {
	return &Converter{
		states: make(map[string]*turnState),
	}
}

// Convert maps an OCS BusEvent to zero or more AEP envelopes.
// eventType is payload.type, props is payload.properties (raw JSON).
func (c *Converter) Convert(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch {
	case eventType == ocsPartUpdated:
		return c.handlePartUpdated(sessionID, props)
	case eventType == ocsPartDelta:
		return c.handlePartDelta(sessionID, props)
	case strings.HasPrefix(eventType, "session.next."):
		return c.convertV2(sessionID, eventType, props)
	default:
		return c.convertV1(sessionID, eventType, props)
	}
}

// --- V2 event handlers ---

func (c *Converter) convertV2(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch eventType {
	case ocsStepStarted:
		return c.handleStepStarted(sessionID, props)
	case ocsStepEnded:
		return c.handleStepEnded(sessionID, props)
	case ocsStepFailed:
		return c.handleStepFailed(sessionID, props)
	case ocsToolCalled:
		return c.handleToolCalled(sessionID, props)
	case ocsToolSuccess:
		return c.handleToolOutcome(sessionID, props, false)
	case ocsToolFailed:
		return c.handleToolOutcome(sessionID, props, true)
	case ocsReasoningStarted:
		st := c.getOrCreateState(sessionID)
		st.reasoningActive = true
		return nil
	case ocsReasoningEnded:
		// Direct lookup: orphan ended without started is a no-op.
		if st, ok := c.states[sessionID]; ok {
			st.reasoningActive = false
		}
		return nil
	default:
		return nil
	}
}

func (c *Converter) handleStepStarted(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Model struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"model"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Model.ModelID != "" {
		st := c.getOrCreateState(sessionID)
		if st.model == "" {
			st.model = evt.Model.ProviderID + "/" + evt.Model.ModelID
		}
	}
	return nil
}

func (c *Converter) handleStepEnded(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Cost   float64 `json:"cost"`
		Tokens struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			Reasoning float64 `json:"reasoning"`
			Cache     struct {
				Read  float64 `json:"read"`
				Write float64 `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	st := c.getOrCreateState(sessionID)
	st.cost += evt.Cost
	st.tokens.input += int64(evt.Tokens.Input)
	st.tokens.output += int64(evt.Tokens.Output)
	st.tokens.reasoning += int64(evt.Tokens.Reasoning)
	st.tokens.cacheRead += int64(evt.Tokens.Cache.Read)
	st.tokens.cacheWrite += int64(evt.Tokens.Cache.Write)
	return nil
}

func (c *Converter) handleStepFailed(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(props, &evt)

	msg := "step failed"
	if evt.Error.Message != "" {
		msg = evt.Error.Message
	}
	// Dedup via consumeDone: if a Done was already emitted for this turn, skip.
	stats, first := c.consumeDone(sessionID)
	if !first {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Error, events.ErrorData{
			Code:    events.ErrCodeInternalError,
			Message: msg,
		}),
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done, events.DoneData{Success: false, Stats: stats}),
	}
}

// handlePartUpdated records part metadata from OCS 1.17+. The event carries the
// authoritative part type; the actual streamed text still arrives later via
// message.part.delta.
func (c *Converter) handlePartUpdated(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Part struct {
			ID        string `json:"id"`
			MessageID string `json:"messageID"`
			Type      string `json:"type"`
			Ignored   bool   `json:"ignored"`
			Tool      string `json:"tool"`
			CallID    string `json:"callID"`
			State     struct {
				Status string         `json:"status"`
				Input  map[string]any `json:"input"`
				Output string         `json:"output"`
				Error  string         `json:"error"`
			} `json:"state"`
		} `json:"part"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Part.ID == "" || evt.Part.Type == "" {
		return nil
	}

	st := c.getOrCreateState(sessionID)
	if st.parts == nil {
		st.parts = make(map[string]partState)
	}
	st.parts[evt.Part.ID] = partState{
		typ:       evt.Part.Type,
		messageID: evt.Part.MessageID,
		ignored:   evt.Part.Ignored,
	}

	if evt.Part.Type != "tool" || evt.Part.CallID == "" {
		return nil
	}

	// OpenCode 1.17 emits the tool lifecycle as repeated message.part.updated
	// mutations (part.type=="tool"; state.status: pending→running→completed/error).
	// The schema's session.next.tool.* events are not yet emitted on the live
	// SSE stream, so this is the canonical tool-call signal.
	if st.seenToolCalls == nil {
		st.seenToolCalls = make(map[string]bool)
	}
	callID := evt.Part.CallID
	s := &evt.Part.State
	input := s.Input
	if input == nil {
		input = map[string]any{}
	}

	switch s.Status {
	case "running":
		if st.seenToolCalls[callID] {
			return nil
		}
		st.seenToolCalls[callID] = true
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolCall, events.ToolCallData{
				ID:    callID,
				Name:  evt.Part.Tool,
				Input: input,
			}),
		}
	case "completed", "error":
		var envs []*events.Envelope
		if !st.seenToolCalls[callID] {
			st.seenToolCalls[callID] = true
			envs = append(envs, events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolCall, events.ToolCallData{
				ID:    callID,
				Name:  evt.Part.Tool,
				Input: input,
			}))
		}
		if s.Status == "completed" {
			envs = append(envs, events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolResult, events.ToolResultData{
				ID:     callID,
				Output: s.Output,
			}))
		} else {
			msg := s.Error
			if msg == "" {
				msg = "tool failed"
			}
			envs = append(envs, events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolResult, events.ToolResultData{
				ID:    callID,
				Error: msg,
			}))
		}
		return envs
	}
	return nil
}

// handlePartDelta handles message.part.delta from OCS 1.15+.
// Routes by explicit field or by part metadata from message.part.updated:
// reasoning parts → Reasoning, non-ignored text parts → MessageDelta.
func (c *Converter) handlePartDelta(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		MessageID string `json:"messageID"`
		PartID    string `json:"partID"`
		Field     string `json:"field"`
		Delta     string `json:"delta"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Delta == "" {
		return nil
	}

	switch evt.Field {
	case "reasoning":
		return reasoningEnvelope(sessionID, evt.PartID, evt.Delta)
	default: // "text" or unspecified
		if st, ok := c.states[sessionID]; ok {
			if part, found := st.parts[evt.PartID]; found {
				return c.handleKnownPartDelta(sessionID, evt.MessageID, evt.PartID, evt.Delta, part)
			}
			if st.reasoningActive {
				return reasoningEnvelope(sessionID, evt.PartID, evt.Delta)
			}
		}
		return messageDeltaEnvelope(sessionID, evt.MessageID, evt.Delta)
	}
}

func (c *Converter) handleKnownPartDelta(sessionID, messageID, partID, delta string, part partState) []*events.Envelope {
	switch part.typ {
	case "reasoning":
		return reasoningEnvelope(sessionID, partID, delta)
	case "text":
		if part.ignored {
			return nil
		}
		if messageID == "" {
			messageID = part.messageID
		}
		return messageDeltaEnvelope(sessionID, messageID, delta)
	default:
		return nil
	}
}

func reasoningEnvelope(sessionID, partID, delta string) []*events.Envelope {
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Reasoning, events.ReasoningData{
			ID:      partID,
			Content: delta,
		}),
	}
}

func messageDeltaEnvelope(sessionID, messageID, delta string) []*events.Envelope {
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.MessageDelta, events.MessageDeltaData{
			MessageID: messageID,
			Content:   delta,
		}),
	}
}

func (c *Converter) handleToolCalled(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		CallID string         `json:"callID"`
		Tool   string         `json:"tool"`
		Input  map[string]any `json:"input"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolCall, events.ToolCallData{
			ID:    evt.CallID,
			Name:  evt.Tool,
			Input: evt.Input,
		}),
	}
}

func (c *Converter) handleToolOutcome(sessionID string, props json.RawMessage, isFailed bool) []*events.Envelope {
	var evt struct {
		CallID  string `json:"callID"`
		Content []any  `json:"content,omitempty"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	data := events.ToolResultData{ID: evt.CallID}
	if isFailed {
		data.Error = "tool failed"
		if evt.Error != nil {
			data.Error = evt.Error.Message
		}
	} else {
		data.Output = contentToString(evt.Content)
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolResult, data),
	}
}

// --- V1 legacy handlers ---

func (c *Converter) convertV1(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch eventType {
	case ocsSessionStatus:
		return c.handleSessionStatus(sessionID, props)
	case ocsSessionIdle:
		return c.handleSessionIdle(sessionID)
	case ocsSessionError:
		return c.handleSessionError(sessionID, props)
	case ocsPermAsked:
		return c.handlePermAsked(sessionID, props)
	case ocsQuestionAsked:
		return c.handleQuestionAsked(sessionID, props)
	default:
		return nil
	}
}

func (c *Converter) handleSessionStatus(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		Status struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		return nil
	}

	switch data.Status.Type {
	case "idle":
		stats, first := c.consumeDone(sessionID)
		if !first {
			return nil
		}
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done,
				events.DoneData{Success: true, Stats: stats}),
		}
	case "busy":
		// busy marks the start of a new turn — reset per-turn accumulation so
		// a prior turn's doneShown/stats do not leak into this one.
		c.resetForNewTurn(sessionID)
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.State,
				map[string]any{"state": "running"}),
		}
	case "retry":
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.State,
				map[string]any{"state": "retry"}),
		}
	default:
		return nil
	}
}

func (c *Converter) handleSessionIdle(sessionID string) []*events.Envelope {
	stats, first := c.consumeDone(sessionID)
	// Dedup: if Done was already emitted (by handleStepFailed,
	// handleSessionError, or an earlier session.status(idle)), skip.
	if !first {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done,
			events.DoneData{Success: true, Stats: stats}),
	}
}

func (c *Converter) handleSessionError(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		Error struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	_ = json.Unmarshal(props, &data)

	msg := "opencode session error"
	if data.Error.Data.Message != "" {
		msg = data.Error.Data.Message
	} else if data.Error.Name != "" {
		msg = data.Error.Name
	}
	// Dedup via consumeDone: if a Done was already emitted for this turn, skip.
	stats, first := c.consumeDone(sessionID)
	if !first {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Error, events.ErrorData{
			Code:    events.ErrCodeInternalError,
			Message: msg,
		}),
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done, events.DoneData{Success: false, Stats: stats}),
	}
}

func (c *Converter) handlePermAsked(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		ID       string         `json:"id"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		return nil
	}

	toolName, _ := data.Metadata["tool"].(string)
	args, _ := json.Marshal(data.Metadata)
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.PermissionRequest,
			events.PermissionRequestData{
				ID:          data.ID,
				ToolName:    toolName,
				Description: toolName,
				Args:        []string{string(args)},
				InputRaw:    json.RawMessage(args),
			}),
	}
}

func (c *Converter) handleQuestionAsked(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		ID        string            `json:"id"`
		Questions []events.Question `json:"questions"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		return nil
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.QuestionRequest,
			events.QuestionRequestData{
				ID:        data.ID,
				Questions: data.Questions,
			}),
	}
}

// --- state helpers ---

// Reset clears all per-session turn state. Call when the OCS process restarts
// to prevent stale state from leaking into the new process lifecycle.
func (c *Converter) Reset() {
	clear(c.states)
}

func (c *Converter) getOrCreateState(sessionID string) *turnState {
	st, ok := c.states[sessionID]
	if !ok {
		st = &turnState{}
		c.states[sessionID] = st
	}
	return st
}

// consumeDone is the single entry point for emitting a Done event. It returns
// the accumulated stats (for DoneData.Stats) and a `first` flag indicating
// whether this is the first Done for the current turn.
//
// Dedup is driven by turnState.doneShown — NOT by whether stats are non-nil.
// This is critical because OCS 1.17+ (V1 protocol) does not emit
// session.next.step.ended events, so usage is never accumulated and stats are
// always nil at idle time. Done must still be emitted in that case (it is the
// turn terminator and unlocks the webchat UI), otherwise the streaming cursor
// spins forever.
//
// On the first call for a turn it marks doneShown and resets reasoningActive
// (a turn boundary ends any in-flight reasoning phase). A subsequent call
// within the same turn returns first=false so callers can suppress duplicate
// Done events. The turn resets when session.status(busy) arrives.
func (c *Converter) consumeDone(sessionID string) (stats map[string]any, first bool) {
	st := c.getOrCreateState(sessionID) // always create so doneShown dedup works
	if st.doneShown {
		return nil, false
	}
	st.doneShown = true
	st.reasoningActive = false // turn boundary ends any in-flight reasoning phase
	return buildStats(st), true
}

// resetForNewTurn clears per-turn accumulation (stats, doneShown, reasoning
// phase) for the next turn. Called when OCS signals a new turn is starting
// (session.status(busy)). The model is preserved across turns.
func (c *Converter) resetForNewTurn(sessionID string) {
	st, ok := c.states[sessionID]
	if !ok {
		return
	}
	model := st.model
	*st = turnState{model: model}
}

// buildStats renders accumulated usage as a Stats map for DoneData.
// Returns nil when no usage was recorded (zero cost and zero tokens).
func buildStats(st *turnState) map[string]any {
	if st.cost == 0 && st.tokens == (tokenAccum{}) {
		return nil
	}
	stats := map[string]any{
		"tokens": map[string]any{
			"input":       st.tokens.input,
			"output":      st.tokens.output,
			"reasoning":   st.tokens.reasoning,
			"cache_read":  st.tokens.cacheRead,
			"cache_write": st.tokens.cacheWrite,
		},
		"cost": st.cost,
	}
	if st.model != "" {
		stats["model_usage"] = map[string]any{
			st.model: map[string]any{
				"input_tokens":  st.tokens.input,
				"output_tokens": st.tokens.output,
			},
		}
	}
	return stats
}

// contentToString converts OCS tool content ([]any) to a string for AEP ToolResult.
func contentToString(content []any) string {
	if len(content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, c := range content {
		parts = append(parts, contentPartToString(c))
	}
	return strings.Join(parts, "\n")
}

func contentPartToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}
