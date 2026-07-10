package codexcli

import (
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

type Mapper struct {
	sessionID string
	seq       atomic.Int64
	tracker   *messageTracker
	// lastUsage tracks the per-turn token usage from thread/tokenUsage/updated
	// (the "last" breakdown). It is reset on turn/started, so it represents the
	// current turn's delta — which is what SessionAccumulator.mergePerTurnStats
	// expects (it does TotalInput += usage, so a per-turn delta accumulates into
	// a correct cumulative total). The "total" breakdown must NOT be used here:
	// it is already cumulative across turns, so feeding it to the accumulator's
	// "+=" would double-count.
	lastUsage *CodexTokenUsage
	// model is the active model name, sourced from model/rerouted or seeded by
	// the configured thread/start model (see SetModel).
	model string
	// turnID is the current turn ID, used to gate token usage updates.
	turnID string
	// contextWindow is the model context window from
	// thread/tokenUsage/updated.tokenUsage.modelContextWindow. Used as the
	// maxTokens source for the get_context_usage control channel.
	contextWindow int64

	// sentTexts tracks the cumulative text/reasoning already sent for each item ID
	// to perform delta-based diff calculations. Sourced under mu.
	sentTexts map[string]string

	// driftCount counts delta prefix mismatches detected by getDeltaText
	// (snapshot prefix ≠ already-sent text). Cumulative across turns for
	// observability — NOT reset by Reset(). Accessed atomically.
	driftCount atomic.Int64

	// mu guards lastUsage, model, contextWindow, turnID, and sentTexts.
	// trackTokenUsage and trackedUsageStats run in the readNotification
	// goroutine; Reset runs in the monitorProcess goroutine; LastContextUsage
	// and SetModel are invoked from worker goroutines (get_context_usage
	// control channel and thread/start). turnID is read in trackTokenUsage and
	// written by Reset/turn/started, so it must be accessed under lock.
	mu sync.Mutex
}

func NewMapper(sessionID string) *Mapper {
	return &Mapper{
		sessionID: sessionID,
		tracker:   newMessageTracker(),
		sentTexts: make(map[string]string),
	}
}

// MapNotification converts a JSON-RPC notification (app-server mode) to AEP envelopes.
func (m *Mapper) MapNotification(method string, params json.RawMessage) []*events.Envelope {
	switch method {
	case "item/started":
		item := parseNotifItem(params)
		if item == nil {
			return nil
		}
		if item.Type == ItemAgentMessage {
			m.tracker.startMessage(item.ID)
			return []*events.Envelope{
				newEnvelope(events.MessageStart, events.MessageStartData{
					ID:          m.tracker.getMessageID(item.ID),
					Role:        "assistant",
					ContentType: ContentTypeText,
				}, m.sessionID, m.nextSeq()),
			}
		}
		return m.mapItemStarted(item)
	case "item/completed":
		item := parseNotifItem(params)
		if item == nil {
			return nil
		}
		if item.Type == ItemAgentMessage {
			envs := []*events.Envelope{
				newEnvelope(events.MessageEnd, events.MessageEndData{
					MessageID: m.tracker.getMessageID(item.ID),
				}, m.sessionID, m.nextSeq()),
			}
			m.tracker.endMessage(item.ID)
			return envs
		}
		return m.mapItemCompleted(item)
	case "item/updated":
		item := parseNotifItem(params)
		if item == nil {
			return nil
		}
		return m.mapItemUpdated(item)
	case "item/agentMessage/delta":
		return m.mapNotifDelta(params)
	case "turn/completed":
		return m.mapNotifTurnCompleted()
	case "turn/failed":
		return m.mapTurnFailed()
	case codexMethodServerRequestApproval,
		codexMethodCommandApproval,
		codexMethodFileChangeApproval,
		codexMethodExecCommandApproval,
		codexMethodApplyPatchApproval:
		return m.mapNotifApproval(params)
	case codexMethodPermissionsApproval:
		return m.mapNotifPermissionsApproval(params)
	case codexMethodRequestUserInput:
		return m.mapNotifRequestUserInput(params)
	case codexMethodMCPElicitation:
		return m.mapNotifElicitation(params)
	case "thread/started":
		return nil
	case "item/reasoning/summaryTextDelta":
		return m.mapNotifReasoningDelta(params)
	case "item/reasoning/textDelta":
		return m.mapNotifReasoningDelta(params)
	case "item/commandExecution/outputDelta":
		return m.mapNotifOutputDelta(params)
	case "item/mcpToolCall/progress":
		return m.mapNotifMCPProgress(params)
	case "thread/tokenUsage/updated":
		m.trackTokenUsage(params)
		return nil
	case "model/rerouted":
		m.trackModelRerouted(params)
		return nil
	case "error":
		return m.mapNotifError(params)
	case "warning":
		return m.mapNotifWarning(params)
	case "turn/started":
		m.mu.Lock()
		m.lastUsage = nil
		m.turnID = extractTurnID(params)
		m.mu.Unlock()
		return nil
	case "turn/diff/updated":
		return m.mapNotifDiffUpdated(params)
	case "turn/plan/updated":
		return m.mapNotifPlanUpdated(params)
	case "thread/settings/updated":
		return m.mapNotifSettingsUpdated(params)
	case "serverRequest/resolved":
		return nil // no user-facing action needed
	case "deprecationNotice":
		return m.mapNotifDeprecation(params)
	case "guardianWarning":
		return m.mapNotifGuardianWarning(params)
	}
	return nil
}

// ─── Shared CodexItem → AEP mapping ──────────────────────────────────────

func (m *Mapper) mapItemStarted(item *CodexItem) []*events.Envelope {
	if item == nil {
		return nil
	}
	switch item.Type {
	case ItemCommandExecution:
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   item.ID,
				Name: "Bash",
				Input: map[string]any{
					"command": item.Command,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case ItemFileChange:
		name, input := codexFileChangeToToolCall(item)
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:    item.ID,
				Name:  name,
				Input: input,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemMCPToolCall:
		var args map[string]any
		if item.Arguments != nil {
			_ = json.Unmarshal(item.Arguments, &args)
		}
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:    item.ID,
				Name:  item.Tool,
				Input: args,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemWebSearch:
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   item.ID,
				Name: "WebSearch",
				Input: map[string]any{
					"query": item.Query,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case ItemCollabToolCall:
		var args map[string]any
		if item.Arguments != nil {
			_ = json.Unmarshal(item.Arguments, &args)
		}
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:    item.ID,
				Name:  item.CollabTool,
				Input: args,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemTodoList:
		return []*events.Envelope{
			newEnvelope(events.Step, events.StepData{
				ID:       item.ID,
				StepType: "planning",
				Name:     "task planning started",
			}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

// codexFileChangeToToolCall maps a CodexItem file_change to Claude Code standard tool name and input.
func codexFileChangeToToolCall(item *CodexItem) (string, map[string]any) {
	name := "Edit"
	if item.Action == "create" {
		name = "Write"
	}
	// Sort file paths for deterministic output on multi-file changes.
	paths := make([]string, 0, len(item.Changes))
	for fp := range item.Changes {
		paths = append(paths, fp)
	}
	slices.Sort(paths)
	return name, map[string]any{"file_path": strings.Join(paths, ", ")}
}

func (m *Mapper) mapItemCompleted(item *CodexItem) []*events.Envelope {
	if item == nil {
		return nil
	}
	switch item.Type {
	case ItemAgentMessage:
		// Dead branch: item/completed dispatch (MapNotification line 87)
		// short-circuits ItemAgentMessage to emit only MessageEnd + endMessage
		// before reaching this method. Retained as defensive fallback in case
		// the dispatch is ever reorganized; the snapshot-diff for agent
		// messages lives exclusively in mapItemUpdated (item/updated path).
		delta := m.getDeltaText(item.ID, item.Text)
		if delta == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.MessageDelta, events.MessageDeltaData{
				MessageID: aep.NewID(),
				Content:   delta,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemReasoning:
		fullText := strings.Join(item.SummaryText, "\n")
		delta := m.getDeltaText(item.ID, fullText)
		if delta == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.Reasoning, events.ReasoningData{
				ID:      item.ID,
				Content: delta,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemCommandExecution:
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: item.Stdout,
				Error:  item.Stderr,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemFileChange:
		status := "completed"
		if item.Status != "completed" {
			status = "failed"
		}
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: status,
				Error:  item.Stderr,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemMCPToolCall:
		var errMsg string
		if item.Error != nil {
			errMsg = item.Error.Message
		}
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: string(item.Result),
				Error:  errMsg,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemPlan:
		return []*events.Envelope{
			newEnvelope(events.State, events.StateData{
				State:   "planning",
				Message: item.Text,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemCollabToolCall:
		var errMsg string
		if item.Error != nil {
			errMsg = item.Error.Message
		}
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: string(item.Result),
				Error:  errMsg,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemWebSearch:
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: string(item.Result),
			}, m.sessionID, m.nextSeq()),
		}
	case ItemTodoList:
		msg := item.Text
		if msg == "" {
			msg = "task planning completed"
		}
		return []*events.Envelope{
			newEnvelope(events.State, events.StateData{
				State:   "planning",
				Message: msg,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemError:
		var errMsg string
		if item.Error != nil {
			errMsg = item.Error.Message
		}
		return []*events.Envelope{
			newEnvelope(events.Error, events.ErrorData{
				Code: "CODEX_ITEM_ERROR", Message: errMsg,
			}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

// mapItemUpdated handles incremental updates to an item (item.updated events).
// It emits deltas for streamed content rather than the full summary emitted on completion.
func (m *Mapper) mapItemUpdated(item *CodexItem) []*events.Envelope {
	if item == nil {
		return nil
	}
	switch item.Type {
	case ItemAgentMessage:
		delta := m.getDeltaText(item.ID, item.Text)
		if delta == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.MessageDelta, events.MessageDeltaData{
				MessageID: m.tracker.getMessageID(item.ID),
				Content:   delta,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemCommandExecution:
		if item.Stdout == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.ToolUpdate, events.ToolUpdateData{
				ID:        item.ID,
				Status:    "in_progress",
				RawOutput: item.Stdout,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemFileChange:
		if item.Stdout == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.ToolUpdate, events.ToolUpdateData{
				ID:        item.ID,
				Status:    "in_progress",
				RawOutput: item.Stdout,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemReasoning:
		fullText := strings.Join(item.SummaryText, "\n")
		delta := m.getDeltaText(item.ID, fullText)
		if delta == "" {
			return nil
		}
		return []*events.Envelope{
			newEnvelope(events.Reasoning, events.ReasoningData{
				ID:      item.ID,
				Content: delta,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemTodoList:
		return []*events.Envelope{
			newEnvelope(events.State, events.StateData{
				State:   "planning",
				Message: "task list updated",
			}, m.sessionID, m.nextSeq()),
		}
	default:
		// CollabToolCall, WebSearch, McpToolCall, Error — only emit on started/completed.
		return nil
	}
}

func (m *Mapper) mapTurnFailed() []*events.Envelope {
	return []*events.Envelope{
		newEnvelope(events.Error, events.ErrorData{
			Code: "TURN_FAILED", Message: "turn failed",
		}, m.sessionID, m.nextSeq()),
		newEnvelope(events.Done, events.DoneData{Success: false}, m.sessionID, m.nextSeq()),
	}
}

// ─── App-server-specific notification handlers ───────────────────────────

func (m *Mapper) mapNotifDelta(params json.RawMessage) []*events.Envelope {
	var p struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	m.recordSentDelta(p.ItemID, p.Delta)
	return []*events.Envelope{
		newEnvelope(events.MessageDelta, events.MessageDeltaData{
			MessageID: m.tracker.getMessageID(p.ItemID),
			Content:   p.Delta,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifTurnCompleted() []*events.Envelope {
	// Turn has no usage field in app-server protocol.
	// Usage comes from thread/tokenUsage/updated, tracked in m.lastUsage.
	return []*events.Envelope{
		newEnvelope(events.Done, events.DoneData{
			Success: true,
			Stats:   m.trackedUsageStats(),
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifApproval(params json.RawMessage) []*events.Envelope {
	// Different approval notification types use different ID fields:
	//   serverRequest/approval                → requestId
	//   item/commandExecution/requestApproval → approvalId (null for regular shell) or itemId
	//   item/fileChange/requestApproval       → itemId
	//   execCommandApproval                   → approvalId or callId
	//   applyPatchApproval                    → callId
	// We pick the first non-empty: approvalId → itemId → requestId → callId.
	var p struct {
		RequestID  string `json:"requestId"`
		ApprovalID string `json:"approvalId"`
		ItemID     string `json:"itemId"`
		CallID     string `json:"callId"`
		ToolName   string `json:"toolName"`
		Command    any    `json:"command"`
		Reason     string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}

	// Resolve canonical request ID.
	requestID := p.ApprovalID
	if requestID == "" {
		requestID = p.ItemID
	}
	if requestID == "" {
		requestID = p.RequestID
	}
	if requestID == "" {
		requestID = p.CallID
	}

	// Resolve tool name: prefer explicit toolName, fall back to command preview.
	toolName := p.ToolName
	if toolName == "" {
		command := approvalCommandPreview(p.Command)
		if command != "" {
			if len(command) > 60 {
				toolName = command[:60] + "…"
			} else {
				toolName = command
			}
		}
	}
	if toolName == "" {
		toolName = "shell command"
	}

	desc := toolName
	if p.Reason != "" {
		desc = p.Reason
	}

	return []*events.Envelope{
		newEnvelope(events.PermissionRequest, events.PermissionRequestData{
			ID:          requestID,
			ToolName:    toolName,
			Description: desc,
		}, m.sessionID, m.nextSeq()),
	}
}

func approvalCommandPreview(command any) string {
	switch v := command.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			if s, ok := part.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func (m *Mapper) mapNotifPermissionsApproval(params json.RawMessage) []*events.Envelope {
	var p struct {
		RequestID   string         `json:"requestId"`
		ItemID      string         `json:"itemId"`
		Reason      string         `json:"reason"`
		CWD         string         `json:"cwd"`
		Permissions map[string]any `json:"permissions"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	requestID := p.ItemID
	if requestID == "" {
		requestID = p.RequestID
	}
	description := p.Reason
	if description == "" {
		description = "Codex requests additional sandbox permissions"
	}
	inputRaw, _ := json.Marshal(map[string]any{
		"cwd":         p.CWD,
		"permissions": p.Permissions,
	})
	return []*events.Envelope{
		newEnvelope(events.PermissionRequest, events.PermissionRequestData{
			ID:          requestID,
			ToolName:    "request_permissions",
			Description: description,
			InputRaw:    inputRaw,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifRequestUserInput(params json.RawMessage) []*events.Envelope {
	var p struct {
		RequestID string `json:"requestId"`
		ItemID    string `json:"itemId"`
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	requestID := p.ItemID
	if requestID == "" {
		requestID = p.RequestID
	}
	questions := make([]events.Question, 0, len(p.Questions))
	for _, question := range p.Questions {
		options := make([]events.QuestionOption, 0, len(question.Options))
		for _, option := range question.Options {
			options = append(options, events.QuestionOption{
				Label:       option.Label,
				Description: option.Description,
			})
		}
		questions = append(questions, events.Question{
			ID:       question.ID,
			Header:   question.Header,
			Question: question.Question,
			Options:  options,
		})
	}
	return []*events.Envelope{
		newEnvelope(events.QuestionRequest, events.QuestionRequestData{
			ID:        requestID,
			ToolName:  "request_user_input",
			Questions: questions,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifElicitation(params json.RawMessage) []*events.Envelope {
	var p struct {
		RequestID       string         `json:"requestId"`
		MCPServerName   string         `json:"mcpServerName"`
		ServerName      string         `json:"serverName"`
		Message         string         `json:"message"`
		Mode            string         `json:"mode,omitempty"`
		URL             string         `json:"url,omitempty"`
		ElicitationID   string         `json:"elicitationId,omitempty"`
		RequestedSchema map[string]any `json:"requestedSchema,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		slog.Warn("codexcli: unmarshal elicitation params", "err", err, "raw", string(params))
		return nil
	}
	if p.MCPServerName == "" {
		p.MCPServerName = p.ServerName
	}

	return []*events.Envelope{
		newEnvelope(events.ElicitationRequest, events.ElicitationRequestData{
			ID:              p.RequestID,
			MCPServerName:   p.MCPServerName,
			Message:         p.Message,
			Mode:            p.Mode,
			URL:             p.URL,
			ElicitationID:   p.ElicitationID,
			RequestedSchema: p.RequestedSchema,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifReasoningDelta(params json.RawMessage) []*events.Envelope {
	var p struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Delta == "" {
		return nil
	}
	m.recordSentDelta(p.ItemID, p.Delta)
	return []*events.Envelope{
		newEnvelope(events.Reasoning, events.ReasoningData{
			ID:      p.ItemID,
			Content: p.Delta,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifMCPProgress(params json.RawMessage) []*events.Envelope {
	var p struct {
		ItemID  string `json:"itemId"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.Step, events.StepData{
			ID:       p.ItemID,
			StepType: "mcp_progress",
			Name:     p.Message,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifWarning(params json.RawMessage) []*events.Envelope {
	var p struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.Step, events.StepData{
			StepType: "warning",
			Name:     p.Message,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifError(params json.RawMessage) []*events.Envelope {
	var p struct {
		Error struct {
			Message           string `json:"message"`
			AdditionalDetails string `json:"additionalDetails"`
		} `json:"error"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	msg := p.Error.Message
	if msg == "" {
		msg = "unknown error"
	}
	// Only emit Error; Done is emitted by turn/failed or turn/completed.
	return []*events.Envelope{
		newEnvelope(events.Error, events.ErrorData{
			Code: "CODEX_ERROR", Message: msg,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifOutputDelta(params json.RawMessage) []*events.Envelope {
	var p struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ItemID == "" {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.ToolResult, events.ToolResultData{
			ID:     p.ItemID,
			Output: p.Delta,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifDiffUpdated(params json.RawMessage) []*events.Envelope {
	var p struct {
		ItemID string `json:"itemId"`
		Lines  string `json:"lines"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.ToolUpdate, events.ToolUpdateData{
			ID:      p.ItemID,
			Status:  "in_progress",
			Content: p.Lines,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifPlanUpdated(params json.RawMessage) []*events.Envelope {
	var p struct {
		Steps json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.State, events.StateData{
			State:   "planning",
			Message: string(p.Steps),
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifSettingsUpdated(params json.RawMessage) []*events.Envelope {
	var p struct {
		Settings json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.State, events.StateData{
			State:   "settings",
			Message: string(p.Settings),
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifDeprecation(params json.RawMessage) []*events.Envelope {
	var p struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		slog.Debug("codexcli: unmarshal deprecationNotice params", "err", err, "raw", string(params))
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.Step, events.StepData{
			StepType: "deprecation",
			Name:     p.Message,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifGuardianWarning(params json.RawMessage) []*events.Envelope {
	var p struct {
		Message  string `json:"message"`
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		slog.Debug("codexcli: unmarshal guardianWarning params", "err", err, "raw", string(params))
		return nil
	}
	slog.Debug("codexcli: guardian warning", "severity", p.Severity, "message", p.Message)
	if p.Severity == "error" {
		return []*events.Envelope{
			newEnvelope(events.Error, events.ErrorData{
				Code: "GUARDIAN_WARNING", Message: p.Message,
			}, m.sessionID, m.nextSeq()),
		}
	}
	return []*events.Envelope{
		newEnvelope(events.Step, events.StepData{
			StepType: "guardian",
			Name:     p.Message,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) trackTokenUsage(params json.RawMessage) {
	var p struct {
		TurnID     string `json:"turnId"`
		TokenUsage struct {
			Last *CodexTokenUsage `json:"last"`
			// modelContextWindow is the model's context window size, emitted by
			// codex alongside the usage breakdowns. Pointer to detect presence
			// and distinguish "0" from "absent".
			ModelContextWindow *int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Only accept usage for the current turn to prevent stale data.
	// turnID must be read under lock: Reset() writes it from a different goroutine.
	if m.turnID != "" && p.TurnID != m.turnID {
		return
	}
	if p.TokenUsage.Last != nil {
		m.lastUsage = p.TokenUsage.Last
	}
	if p.TokenUsage.ModelContextWindow != nil && *p.TokenUsage.ModelContextWindow > 0 {
		m.contextWindow = *p.TokenUsage.ModelContextWindow
	}
}

func (m *Mapper) trackModelRerouted(params json.RawMessage) {
	var p struct {
		ToModel string `json:"toModel"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = p.ToModel
}

func (m *Mapper) trackedUsageStats() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	// delta_drifts is read atomically and is NOT reset by Reset() — it reports
	// cumulative drift since the Mapper was created. Include it in the early
	// return decision so drift-only turns (no usage, no model) still surface.
	drifts := m.driftCount.Load()
	if m.lastUsage == nil && m.model == "" && drifts == 0 {
		return nil
	}
	stats := make(map[string]any)
	if m.lastUsage != nil {
		usage := map[string]any{
			"input_tokens":  int64(m.lastUsage.InputTokens),
			"output_tokens": int64(m.lastUsage.OutputTokens),
		}
		if m.lastUsage.CachedInputTokens > 0 {
			// Codex's cachedInputTokens maps to Anthropic's cache_read_input_tokens.
			// cache_creation_input_tokens is intentionally absent: codex's
			// TokenUsageBreakdown does not expose a cache-creation dimension
			// (protocol limitation; see Worker-Turn-Summary-Parity-Spec.md §3).
			usage["cache_read_input_tokens"] = int64(m.lastUsage.CachedInputTokens)
		}
		stats["usage"] = usage
	}
	if m.model != "" {
		// Carry contextWindow in model_usage so SessionAccumulator can populate
		// ContextWindow from the Done stats path (mirrors claudecode's format),
		// in addition to the get_context_usage control channel.
		mu := map[string]any{}
		if m.contextWindow > 0 {
			mu["contextWindow"] = m.contextWindow
		}
		stats["model_usage"] = map[string]any{
			m.model: mu,
		}
	}
	// delta_drifts is cumulative across turns (driftCount is NOT reset by
	// Reset), so this reports total drift since the Mapper was created.
	if drifts > 0 {
		stats["delta_drifts"] = drifts
	}
	return stats
}

// SetModel seeds the active model name. Codex only emits model/rerouted when a
// request is rerouted, so normal conversations would otherwise leave model_name
// empty. The manager calls this on thread/start with the configured model.
func (m *Mapper) SetModel(model string) {
	if model == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = model
}

// LastContextUsage returns context usage for the get_context_usage control
// channel in the camelCase shape expected by events.MapContextUsageResponse.
//
// Sourced entirely from thread/tokenUsage/updated (codex does not expose usage
// via thread/read — its Turn payload carries no token counts):
//   - totalTokens: last turn's input + cached input (approximates context fill)
//   - maxTokens:   tokenUsage.modelContextWindow
//   - model:       tracked active model
//
// Returns an empty map when no usage has been observed yet; the caller
// (bridge_forward mergeContextUsage) tolerates zero values.
func (m *Mapper) LastContextUsage() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]any{}
	if m.lastUsage != nil {
		result["totalTokens"] = m.lastUsage.InputTokens + m.lastUsage.CachedInputTokens
	}
	if m.contextWindow > 0 {
		result["maxTokens"] = m.contextWindow
	}
	if m.model != "" {
		result["model"] = m.model
	}
	return result
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (m *Mapper) nextSeq() int64 {
	return m.seq.Add(1)
}

// Reset clears internal tracking state. Kept for direct unit tests; production
// lifecycle uses CodexAppServerManager.deleteConverter / clearConverters.
func (m *Mapper) Reset() {
	m.mu.Lock()
	m.lastUsage = nil
	m.model = ""
	m.turnID = ""
	m.contextWindow = 0
	m.sentTexts = make(map[string]string)
	m.mu.Unlock()
	m.tracker.Reset()
}

// getDeltaText computes the newly appended characters for a given item ID
// by comparing the current text against the previously recorded sent text.
// It updates the recorded text to reflect the new state.
//
// Drift handling: the codex app-server emits two heterogenous streams sharing
// one item ID — item/agentMessage/delta (true incremental deltas, recorded via
// recordSentDelta) and item/updated (full snapshots so far). Naïve rune-length
// tail diff assumes snapshot.HasPrefix(sentTexts). When this invariant breaks
// (backend re-sampling/correction/truncation), the old impl silently dropped
// (currLen<=lastLen) or returned a misaligned tail slice.
//
// This version validates the prefix. On mismatch it:
//  1. Computes the longest common prefix of sentTexts[id] and currentText.
//  2. Sends currentText[commonPrefix:] as a corrective delta so downstream
//     append-only accumulators at least converge on the correct suffix.
//  3. Resets sentTexts[id] = currentText (re-baseline to the snapshot).
//  4. Increments driftCount (exposed via trackedUsageStats → DoneData.Stats
//     as delta_drifts) and logs a Warning outside m.mu for observability.
//
// Limit: mapper cannot retract the already-emitted erroneous prefix from
// downstream accumulators. Corrective delta guarantees future deltas are
// based on the correct snapshot; it does not retroactively fix displayed text.
//
// Access to m.sentTexts is guarded under m.mu.
func (m *Mapper) getDeltaText(itemID, currentText string) string {
	m.mu.Lock()

	if m.sentTexts == nil {
		m.sentTexts = make(map[string]string)
	}

	sentRunes := []rune(m.sentTexts[itemID])
	currRunes := []rune(currentText)
	lastLen := len(sentRunes)
	currLen := len(currRunes)

	// Empty snapshot has no semantic content; preserve the existing baseline
	// so a subsequent non-empty snapshot can diff against already-sent text.
	// Without this guard, an empty snapshot would re-baseline sentTexts to "",
	// causing the next snapshot to re-emit the full text as a "corrective"
	// delta and corrupt downstream append-only accumulators.
	if currLen == 0 {
		m.mu.Unlock()
		return ""
	}

	// Compute the common prefix once — used both for the consistency check
	// and (on drift) for the corrective slice.
	common := commonPrefixLen(sentRunes, currRunes)

	var delta string
	var drifted bool
	switch {
	case currLen == lastLen && common == lastLen:
		// Idempotent: snapshot == already-sent, no new content.
		delta = ""
	case common == lastLen:
		// Normal append: snapshot is sent + new tail (currLen > lastLen here,
		// since currLen==lastLen with common==lastLen is handled above).
		delta = string(currRunes[lastLen:])
		m.sentTexts[itemID] += delta
	default:
		// Prefix drift (Type A: shorter/divergent; Type B: longer but prefix
		// inconsistent). Send snapshot[common:] as a corrective delta and
		// re-baseline to the snapshot. In the strict-truncation sub-case
		// (snapshot is a proper prefix of sent, common==currLen<lastLen) the
		// corrective is "" but we still re-baseline + count it as drift so
		// operators see the backend's truncation event.
		delta = string(currRunes[common:])
		m.sentTexts[itemID] = string(currRunes)
		m.driftCount.Add(1)
		drifted = true
	}
	m.mu.Unlock()

	// Log outside m.mu: slog.Warn may do blocking I/O; under a misbehaving
	// backend that drifts every snapshot, holding the lock through the log
	// call would serialize all mapper operations.
	if drifted {
		slog.Warn("codexcli: delta prefix drift detected, sending corrective delta",
			"item_id", itemID,
			"sent_len", lastLen,
			"snapshot_len", currLen,
			"common_prefix", common)
	}
	return delta
}

// DriftCount returns the cumulative number of delta prefix mismatches
// detected since the Mapper was created. Not reset by Reset() — cumulative
// across turns for observability. Safe to call concurrently with getDeltaText.
func (m *Mapper) DriftCount() int64 {
	return m.driftCount.Load()
}

// commonPrefixLen returns the length of the longest common prefix of two rune
// slices. Zero if the first runes differ or either slice is empty.
func commonPrefixLen(a, b []rune) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// recordSentDelta appends the sent delta string for a given item ID
// after delta content has been sent via another event source (like delta notification).
func (m *Mapper) recordSentDelta(itemID, delta string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sentTexts == nil {
		m.sentTexts = make(map[string]string)
	}

	m.sentTexts[itemID] += delta
}

func newEnvelope(kind events.Kind, data interface{}, sessionID string, seq int64) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, seq, kind, data)
}

// parseNotifItem unmarshals a JSON-RPC notification's "item" field into a CodexItem.
func parseNotifItem(params json.RawMessage) *CodexItem {
	var p struct {
		Item *CodexItem `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return p.Item
}

func extractTurnID(params json.RawMessage) string {
	var p struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Turn.ID
}

// ─── messageTracker ─────────────────────────────────────────────────────

type messageTracker struct {
	mu       sync.Mutex
	messages map[string]string // itemID → messageID
}

func newMessageTracker() *messageTracker {
	return &messageTracker{messages: make(map[string]string)}
}

func (t *messageTracker) startMessage(itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages[itemID] = aep.NewID()
}

func (t *messageTracker) getMessageID(itemID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.messages[itemID]; ok {
		return id
	}
	id := aep.NewID()
	t.messages[itemID] = id
	return id
}

func (t *messageTracker) endMessage(itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.messages, itemID)
}

// Reset clears all tracked messages, called on session end or crash recovery.
func (t *messageTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = make(map[string]string, len(t.messages))
}
