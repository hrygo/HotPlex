package acp

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/metrics"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── ACP ↔ AEP Mapper ────────────────────────────────────────────────────────

// ACPMapper converts ACP session/update notifications into AEP Envelopes.
// It maintains minimal state for synthetic event generation (message.start/end, state).
type ACPMapper struct {
	sessionID  string
	userID     string
	log        *slog.Logger
	msgActive  atomic.Bool // true when inside a message stream (first chunk received, not yet ended)
	turnActive atomic.Bool // true while a prompt turn is in progress
	seq        atomic.Int64
	msgID      string // pre-computed "msg_" + sessionID, avoids allocation on hot path

	// usageSnapshot caches the latest usage_update for ControlRequester queries.
	// Accumulated across multiple usage_update notifications within a turn.
	usageMu sync.Mutex
	usage   usageSnapshot
}

// usageSnapshot caches token usage, context size, and cost from usage_update events.
// Embeds PromptUsage for token fields; adds context/cost fields from usage_update.
type usageSnapshot struct {
	PromptUsage
	ContextSize int
	ContextUsed int
	Cost        *CostInfo
}

// CostInfo holds cost information from usage_update events.
type CostInfo struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// NewACPMapper creates a mapper bound to the given session.
func NewACPMapper(sessionID, userID string, log *slog.Logger) *ACPMapper {
	if log == nil {
		log = slog.Default()
	}
	return &ACPMapper{
		sessionID: sessionID,
		userID:    userID,
		log:       log,
		msgID:     "msg_" + sessionID,
	}
}

// Reset clears turn state. Called before each session/prompt.
func (m *ACPMapper) Reset() {
	m.msgActive.Store(false)
	m.turnActive.Store(false)
	m.usageMu.Lock()
	m.usage = usageSnapshot{}
	m.usageMu.Unlock()
}

// SetTurnActive marks the start of a prompt turn.
func (m *ACPMapper) SetTurnActive() { m.turnActive.Store(true) }

// MapNotification converts an ACP notification to zero or more AEP Envelopes.
// Optimized: discriminator + hot-path content (message/thought chunks) extracted
// in a single unmarshal pass, eliminating triple-deserialization on the streaming path.
func (m *ACPMapper) MapNotification(notif *JSONRPCNotification) []*events.Envelope {
	if notif.Method != "session/update" {
		m.log.Debug("acp mapper: skipping non-session/update notification", "method", notif.Method)
		return nil
	}

	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(notif.Params, &params); err != nil {
		m.log.Debug("acp mapper: failed to parse session/update params", "error", err)
		return nil
	}

	// Single-pass discriminator + hot-path content extraction.
	var disc struct {
		SessionUpdate string         `json:"sessionUpdate"`
		Content       acpTextContent `json:"content"` // populated for chunk types only
	}
	if err := json.Unmarshal(params.Update, &disc); err != nil {
		return nil
	}

	switch disc.SessionUpdate {
	case "agent_message_chunk":
		return m.mapAgentMessageChunkText(disc.Content.Text)
	case "agent_thought_chunk":
		return m.mapAgentThoughtChunkText(disc.Content.Text)
	case "tool_call":
		return m.mapToolCall(params.Update)
	case "tool_call_update":
		return m.mapToolCallUpdate(params.Update)
	case "usage_update":
		m.updateUsage(params.Update)
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
		m.log.Warn("acp mapper: unknown sessionUpdate type, skipping",
			"type", disc.SessionUpdate)
		return nil
	}
}

// MapPromptResponse converts a prompt result to AEP done + message.end envelopes.
func (m *ACPMapper) MapPromptResponse(result *PromptResult) []*events.Envelope {
	envs := m.closeMessageStream()

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
	envs := m.closeMessageStream()

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

// closeMessageStream emits a message.end envelope if a message stream is active.
func (m *ACPMapper) closeMessageStream() []*events.Envelope {
	if !m.msgActive.Swap(false) {
		return nil
	}
	return []*events.Envelope{
		m.newEnvelope(events.MessageEnd, events.MessageEndData{
			MessageID: m.msgID,
		}),
	}
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

func (m *ACPMapper) mapAgentMessageChunkText(text string) []*events.Envelope {
	if text == "" {
		return nil
	}

	var envs []*events.Envelope

	// First chunk → synthesize message.start
	if !m.msgActive.Swap(true) {
		envs = append(envs, m.newEnvelope(events.MessageStart, events.MessageStartData{
			ID:          m.msgID,
			Role:        "assistant",
			ContentType: "text",
		}))
	}

	envs = append(envs, m.newEnvelope(events.MessageDelta, events.MessageDeltaData{
		MessageID: m.msgID,
		Content:   text,
	}))
	return envs
}

func (m *ACPMapper) mapAgentThoughtChunkText(text string) []*events.Envelope {
	if text == "" {
		return nil
	}
	return []*events.Envelope{
		m.newEnvelope(events.Reasoning, events.ReasoningData{
			ID:      aep.NewID(),
			Content: text,
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

	var input map[string]any
	if len(u.RawInput) > 0 {
		_ = json.Unmarshal(u.RawInput, &input)
	}
	if input == nil {
		input = make(map[string]any)
	}

	// Extract tool name and file locations from content.
	type contentItem struct {
		Path string `json:"path"`
		Line int    `json:"line,omitempty"`
	}
	var items []contentItem
	var toolName string
	if len(u.Content) > 0 {
		// Try _meta.claudeCode.toolName first.
		toolName = extractToolName(u.Content)
		// Also extract file location items.
		_ = json.Unmarshal(u.Content, &items)
	}
	// Fallback: use Kind or Title when toolName is empty (non-ClaudeCode ACP agents).
	if toolName == "" {
		toolName = u.Kind
	}
	if toolName == "" {
		toolName = u.Title
	}

	var locations []events.FileLocation
	for _, item := range items {
		if item.Path != "" {
			locations = append(locations, events.FileLocation{
				Path: item.Path,
				Line: item.Line,
			})
		}
	}

	data := events.ToolCallData{
		ID:        u.ToolCallID,
		Name:      toolName,
		Input:     input,
		Title:     u.Title,
		Kind:      u.Kind,
		Locations: locations,
	}

	// Record Prometheus tool call metric.
	toolKind := u.Kind
	if toolKind == "" {
		toolKind = "other"
	}
	metrics.ACPToolCallsTotal.WithLabelValues(toolKind).Inc()

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
	return events.NewEnvelope(aep.NewID(), m.sessionID, m.nextSeq(), kind, data)
}

func (m *ACPMapper) buildStats(result *PromptResult) map[string]any {
	stats := make(map[string]any)
	if result == nil {
		return stats
	}
	stats["stop_reason"] = result.StopReason

	// Wrap token data in nested "usage" map for compatibility with the
	// gateway's sessionAccumulator.mergePerTurnStats (Claude Code format).
	// Keep the key names matching Anthropic API fields so the shared
	// accumulator can extract them without a third format branch.
	usage := map[string]any{}
	if result.Usage.InputTokens > 0 {
		usage["input_tokens"] = result.Usage.InputTokens
	}
	if result.Usage.OutputTokens > 0 {
		usage["output_tokens"] = result.Usage.OutputTokens
	}
	if result.Usage.ThoughtTokens > 0 {
		usage["thought_tokens"] = result.Usage.ThoughtTokens
	}
	if result.Usage.CachedReadTokens > 0 {
		usage["cache_read_input_tokens"] = result.Usage.CachedReadTokens
	}
	if result.Usage.CachedWriteTokens > 0 {
		usage["cache_creation_input_tokens"] = result.Usage.CachedWriteTokens
	}
	if result.Usage.TotalTokens > 0 {
		usage["total_tokens"] = result.Usage.TotalTokens
	}
	if len(usage) > 0 {
		stats["usage"] = usage
	}

	// Include accumulated usage_update data (context size, cost).
	// Cost stored as float64 for compatibility with events.ToFloat64().
	m.usageMu.Lock()
	if m.usage.ContextSize > 0 {
		stats["context_size"] = m.usage.ContextSize
	}
	if m.usage.ContextUsed > 0 {
		stats["context_used"] = m.usage.ContextUsed
	}
	if m.usage.Cost != nil && m.usage.Cost.Amount > 0 {
		stats["total_cost_usd"] = m.usage.Cost.Amount
	}
	m.usageMu.Unlock()

	return stats
}

// ─── Usage Tracking ──────────────────────────────────────────────────────────

// updateUsage parses a usage_update notification and accumulates into the snapshot.
func (m *ACPMapper) updateUsage(raw json.RawMessage) {
	var u struct {
		InputTokens       int `json:"inputTokens"`
		OutputTokens      int `json:"outputTokens"`
		ThoughtTokens     int `json:"thoughtTokens"`
		CachedReadTokens  int `json:"cachedReadTokens"`
		CachedWriteTokens int `json:"cachedWriteTokens"`
		TotalTokens       int `json:"totalTokens"`
		ContextSize       int `json:"contextSize"`
		ContextUsed       int `json:"contextUsed"`
		Cost              *struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"cost"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		m.log.Debug("acp mapper: failed to parse usage_update", "error", err)
		return
	}

	// Accumulate into snapshot under lock (field names match PromptUsage via embedding).
	m.usageMu.Lock()
	s := &m.usage
	s.InputTokens += u.InputTokens
	s.OutputTokens += u.OutputTokens
	s.ThoughtTokens += u.ThoughtTokens
	s.CachedReadTokens += u.CachedReadTokens
	s.CachedWriteTokens += u.CachedWriteTokens
	s.TotalTokens += u.TotalTokens
	if u.ContextSize > 0 {
		s.ContextSize = u.ContextSize
	}
	if u.ContextUsed > 0 {
		s.ContextUsed = u.ContextUsed
	}
	if u.Cost != nil {
		if s.Cost == nil {
			s.Cost = &CostInfo{}
		}
		s.Cost.Amount += u.Cost.Amount
		s.Cost.Currency = u.Cost.Currency
	}
	m.usageMu.Unlock()

	// Prometheus metrics outside the lock — counters have internal synchronization.
	for label, val := range map[string]int{
		"input":        u.InputTokens,
		"output":       u.OutputTokens,
		"thought":      u.ThoughtTokens,
		"cached_read":  u.CachedReadTokens,
		"cached_write": u.CachedWriteTokens,
	} {
		if val > 0 {
			metrics.ACPPromptTokensTotal.WithLabelValues(label).Add(float64(val))
		}
	}
}

// LastUsage returns a copy of the current usage snapshot for ControlRequester queries.
func (m *ACPMapper) LastUsage() usageSnapshot {
	m.usageMu.Lock()
	snap := m.usage
	m.usageMu.Unlock()
	if snap.Cost != nil {
		cp := *snap.Cost
		snap.Cost = &cp
	}
	return snap
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

// isAllowKind reports whether the option kind is an "allow" variant.
// See ACP spec: session/request_permission → options[].kind.
func isAllowKind(kind string) bool {
	return kind == "allow_once" || kind == "allow_always" || kind == "allow_session"
}

// isDenyKind reports whether the option kind is a "reject" variant.
// See ACP spec: session/request_permission → options[].kind.
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
