package acp

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── ACP ↔ AEP Mapper ────────────────────────────────────────────────────────

// ACPMapper converts ACP session/update notifications into AEP Envelopes.
// It maintains minimal state for synthetic event generation (message.start/end, state).
type ACPMapper struct {
	sessionID  string
	userID     string
	msgActive  atomic.Bool // true when inside a message stream (first chunk received, not yet ended)
	turnActive atomic.Bool // true while a prompt turn is in progress
	seq        atomic.Int64
}

// NewACPMapper creates a mapper bound to the given session.
func NewACPMapper(sessionID, userID string) *ACPMapper {
	return &ACPMapper{
		sessionID: sessionID,
		userID:    userID,
	}
}

// Reset clears turn state. Called before each session/prompt.
func (m *ACPMapper) Reset() {
	m.msgActive.Store(false)
	m.turnActive.Store(false)
}

// SetTurnActive marks the start of a prompt turn.
func (m *ACPMapper) SetTurnActive() { m.turnActive.Store(true) }

// MapNotification converts an ACP notification to zero or more AEP Envelopes.
func (m *ACPMapper) MapNotification(notif *JSONRPCNotification) []*events.Envelope {
	if notif.Method != "session/update" {
		return nil
	}

	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(notif.Params, &params); err != nil {
		return nil
	}

	// Extract the sessionUpdate discriminator.
	var discriminator struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(params.Update, &discriminator); err != nil {
		return nil
	}

	switch discriminator.SessionUpdate {
	case "agent_message_chunk":
		return m.mapAgentMessageChunk(params.Update)
	case "agent_thought_chunk":
		return m.mapAgentThoughtChunk(params.Update)
	case "tool_call":
		return m.mapToolCall(params.Update)
	case "tool_call_update":
		return m.mapToolCallUpdate(params.Update)
	case "usage_update":
		return nil // internal tracking, stats included in Done
	case "plan":
		return m.mapPlan(params.Update)
	case "current_mode_update":
		return m.mapModeUpdate(params.Update)
	case "available_commands_update":
		return m.mapRawPassthrough("acp.available_commands_update", notif.Params)
	case "config_option_update":
		return m.mapRawPassthrough("acp.config_option_update", notif.Params)
	case "session_info_update":
		return m.mapRawPassthrough("acp.session_info_update", notif.Params)
	case "user_message_chunk":
		return nil // echo of user input, ignored
	default:
		return nil
	}
}

// MapPromptResponse converts a prompt result to AEP done + message.end envelopes.
func (m *ACPMapper) MapPromptResponse(result *PromptResult) []*events.Envelope {
	var envs []*events.Envelope

	if m.msgActive.Swap(false) {
		envs = append(envs, m.newEnvelope(events.MessageEnd, events.MessageEndData{
			MessageID: m.messageID(),
		}))
	}

	success := result.StopReason == "end_turn" || result.StopReason == "cancelled"
	stats := m.buildStats(result)
	envs = append(envs, m.newEnvelope(events.Done, events.DoneData{
		Success: success,
		Stats:   stats,
	}))

	m.turnActive.Store(false)
	return envs
}

// MapPromptError converts a prompt error to AEP error + done envelopes.
func (m *ACPMapper) MapPromptError(err error) []*events.Envelope {
	var envs []*events.Envelope

	if m.msgActive.Swap(false) {
		envs = append(envs, m.newEnvelope(events.MessageEnd, events.MessageEndData{
			MessageID: m.messageID(),
		}))
	}

	envs = append(envs, m.newEnvelope(events.Error, events.ErrorData{
		Code:    events.ErrCodeInternalError,
		Message: err.Error(),
	}))
	envs = append(envs, m.newEnvelope(events.Done, events.DoneData{
		Success: false,
	}))

	m.turnActive.Store(false)
	return envs
}

// MapStateRunning creates a state(running) envelope.
func (m *ACPMapper) MapStateRunning() *events.Envelope {
	return m.newEnvelope(events.State, events.StateData{
		State: events.StateRunning,
	})
}

// MapStateIdle creates a state(idle) envelope.
func (m *ACPMapper) MapStateIdle() *events.Envelope {
	return m.newEnvelope(events.State, events.StateData{
		State: events.StateIdle,
	})
}

// ─── Individual Mappers ──────────────────────────────────────────────────────

// acpTextContent is the content structure for agent_message_chunk and agent_thought_chunk.
type acpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (m *ACPMapper) mapAgentMessageChunk(raw json.RawMessage) []*events.Envelope {
	var u struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}

	var content acpTextContent
	if err := json.Unmarshal(u.Content, &content); err != nil {
		return nil
	}
	if content.Text == "" {
		return nil
	}

	var envs []*events.Envelope

	// First chunk → synthesize message.start
	if !m.msgActive.Swap(true) {
		envs = append(envs, m.newEnvelope(events.MessageStart, events.MessageStartData{
			ID:          m.messageID(),
			Role:        "assistant",
			ContentType: "text",
		}))
	}

	envs = append(envs, m.newEnvelope(events.MessageDelta, events.MessageDeltaData{
		MessageID: m.messageID(),
		Content:   content.Text,
	}))
	return envs
}

func (m *ACPMapper) mapAgentThoughtChunk(raw json.RawMessage) []*events.Envelope {
	var u struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}

	var content acpTextContent
	if err := json.Unmarshal(u.Content, &content); err != nil {
		return nil
	}
	if content.Text == "" {
		return nil
	}
	return []*events.Envelope{
		m.newEnvelope(events.Reasoning, events.ReasoningData{
			ID:      aep.NewID(),
			Content: content.Text,
		}),
	}
}

// acpToolCallUpdate is the common structure for tool_call and tool_call_update.
type acpToolCallUpdate struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawOutput  string          `json:"rawOutput,omitempty"`
	Diff       json.RawMessage `json:"diff,omitempty"`
}

func (m *ACPMapper) mapToolCall(raw json.RawMessage) []*events.Envelope {
	var u acpToolCallUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}

	name := extractToolName(u.Content)

	var input map[string]any
	if len(u.RawInput) > 0 {
		_ = json.Unmarshal(u.RawInput, &input)
	}
	if input == nil {
		input = make(map[string]any)
	}

	var locations []events.FileLocation
	var contentItems []struct {
		Path string `json:"path"`
		Line int    `json:"line,omitempty"`
	}
	if len(u.Content) > 0 {
		_ = json.Unmarshal(u.Content, &contentItems)
		for _, item := range contentItems {
			if item.Path != "" {
				locations = append(locations, events.FileLocation{
					Path: item.Path,
					Line: item.Line,
				})
			}
		}
	}

	data := events.ToolCallData{
		ID:        u.ToolCallID,
		Name:      name,
		Input:     input,
		Title:     u.Title,
		Kind:      u.Kind,
		Locations: locations,
	}
	return []*events.Envelope{m.newEnvelope(events.ToolCall, data)}
}

func (m *ACPMapper) mapToolCallUpdate(raw json.RawMessage) []*events.Envelope {
	var u acpToolCallUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}

	switch u.Status {
	case "pending", "in_progress":
		return []*events.Envelope{m.newEnvelope(events.ToolUpdate, events.ToolUpdateData{
			ID:     u.ToolCallID,
			Status: u.Status,
		})}
	case "completed", "failed":
		var diff *events.FileDiff
		if len(u.Diff) > 0 {
			var fd events.FileDiff
			if err := json.Unmarshal(u.Diff, &fd); err == nil {
				diff = &fd
			}
		}
		return []*events.Envelope{m.newEnvelope(events.ToolResult, events.ToolResultData{
			ID:     u.ToolCallID,
			Output: u.RawOutput,
			Status: u.Status,
			Diff:   diff,
		})}
	default:
		// No status but has rawOutput → treat as completed (compat).
		if u.RawOutput != "" {
			return []*events.Envelope{m.newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     u.ToolCallID,
				Output: u.RawOutput,
			})}
		}
		return nil
	}
}

func (m *ACPMapper) mapPlan(raw json.RawMessage) []*events.Envelope {
	var u struct {
		Entries []struct {
			Content  string `json:"content"`
			Priority string `json:"priority"`
			Status   string `json:"status"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	items := make([]events.PlanItem, 0, len(u.Entries))
	for _, e := range u.Entries {
		items = append(items, events.PlanItem{
			Content:  e.Content,
			Priority: e.Priority,
			Status:   e.Status,
		})
	}
	return []*events.Envelope{m.newEnvelope(events.Plan, events.PlanData{Items: items})}
}

func (m *ACPMapper) mapModeUpdate(raw json.RawMessage) []*events.Envelope {
	var u struct {
		CurrentModeID string `json:"currentModeId"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	if u.CurrentModeID == "" {
		return nil
	}
	return []*events.Envelope{m.newEnvelope(events.ModeUpdate, events.ModeUpdateData{
		CurrentModeID: u.CurrentModeID,
	})}
}

func (m *ACPMapper) mapRawPassthrough(kind string, raw json.RawMessage) []*events.Envelope {
	var data any
	_ = json.Unmarshal(raw, &data)
	return []*events.Envelope{m.newEnvelope(events.Raw, events.RawData{
		Kind: kind,
		Raw:  data,
	})}
}

// MapPermissionRequest converts an ACP request_permission to an AEP permission_request.
func (m *ACPMapper) MapPermissionRequest(req *JSONRPCRequest) *PermissionMapResult {
	var params struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string          `json:"toolCallId"`
			Title      string          `json:"title"`
			Kind       string          `json:"kind"`
			RawInput   json.RawMessage `json:"rawInput"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil
	}

	// Find first allow_* and reject_* options.
	desc := ""
	var allowOptionID, denyOptionID string
	for _, opt := range params.Options {
		if allowOptionID == "" && isAllowKind(opt.Kind) {
			desc = opt.Name
			allowOptionID = opt.OptionID
		}
		if denyOptionID == "" && isDenyKind(opt.Kind) {
			denyOptionID = opt.OptionID
		}
	}

	toolName := params.ToolCall.Kind
	if toolName == "" {
		toolName = params.ToolCall.Title
	}

	env := m.newEnvelope(events.PermissionRequest, events.PermissionRequestData{
		ID:          string(req.ID),
		ToolName:    toolName,
		Description: desc,
		InputRaw:    params.ToolCall.RawInput,
	})

	return &PermissionMapResult{
		Envelope:      env,
		RequestID:     req.ID,
		AllowOptionID: allowOptionID,
		DenyOptionID:  denyOptionID,
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (m *ACPMapper) nextSeq() int64 {
	return m.seq.Add(1)
}

func (m *ACPMapper) newEnvelope(kind events.Kind, data any) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       m.nextSeq(),
		SessionID: m.sessionID,
		Timestamp: time.Now().UnixMilli(),
		Event: events.Event{
			Type: kind,
			Data: data,
		},
	}
}

func (m *ACPMapper) messageID() string {
	return "msg_" + m.sessionID
}

func (m *ACPMapper) buildStats(result *PromptResult) map[string]any {
	stats := make(map[string]any)
	if result == nil {
		return stats
	}
	stats["stop_reason"] = result.StopReason
	if result.Usage.InputTokens > 0 {
		stats["input_tokens"] = result.Usage.InputTokens
	}
	if result.Usage.OutputTokens > 0 {
		stats["output_tokens"] = result.Usage.OutputTokens
	}
	if result.Usage.ThoughtTokens > 0 {
		stats["thought_tokens"] = result.Usage.ThoughtTokens
	}
	if result.Usage.CachedReadTokens > 0 {
		stats["cached_read_tokens"] = result.Usage.CachedReadTokens
	}
	if result.Usage.CachedWriteTokens > 0 {
		stats["cached_write_tokens"] = result.Usage.CachedWriteTokens
	}
	if result.Usage.TotalTokens > 0 {
		stats["total_tokens"] = result.Usage.TotalTokens
	}
	return stats
}

func extractToolName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try _meta.claudeCode.toolName.
	var wrapper struct {
		Meta struct {
			ClaudeCode struct {
				ToolName string `json:"toolName"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Meta.ClaudeCode.ToolName != "" {
		return wrapper.Meta.ClaudeCode.ToolName
	}
	return ""
}

func isAllowKind(kind string) bool {
	return kind == "allow_once" || kind == "allow_always" || kind == "allow_session"
}

func isDenyKind(kind string) bool {
	return kind == "reject_once" || kind == "reject_always"
}

// ─── Result Types ────────────────────────────────────────────────────────────

// PermissionMapResult holds the result of mapping an ACP permission request.
type PermissionMapResult struct {
	Envelope      *events.Envelope
	RequestID     json.RawMessage // original JSON-RPC request id (for response)
	AllowOptionID string          // optionId of the first allow_* option
	DenyOptionID  string          // optionId of the first reject_* option
}

// FormatAllowedOutcome builds the ACP outcome for an allowed permission response.
func (r *PermissionMapResult) FormatAllowedOutcome() map[string]any {
	return map[string]any{
		"outcome":  "selected",
		"optionId": r.AllowOptionID,
	}
}

// FormatDeniedOutcome builds the ACP outcome for a denied permission response.
func (r *PermissionMapResult) FormatDeniedOutcome() map[string]any {
	if r.DenyOptionID != "" {
		return map[string]any{
			"outcome":  "selected",
			"optionId": r.DenyOptionID,
		}
	}
	return map[string]any{
		"outcome": "cancelled",
	}
}
