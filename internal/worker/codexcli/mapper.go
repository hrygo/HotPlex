package codexcli

import (
	"encoding/json"
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
	lastUsage *CodexTokenUsage // tracked from thread/tokenUsage/updated
	model     string           // tracked from model/rerouted
	turnID    string           // current turn ID, used to gate token usage updates
}

func NewMapper(sessionID string) *Mapper {
	return &Mapper{
		sessionID: sessionID,
		tracker:   newMessageTracker(),
	}
}

// Map converts a CodexEvent (exec mode JSONL) to AEP envelopes.
func (m *Mapper) Map(event *CodexEvent) []*events.Envelope {
	switch event.Type {
	case EventItemStarted:
		return m.mapItemStarted(event.Item)
	case EventItemCompleted:
		return m.mapItemCompleted(event.Item)
	case EventTurnCompleted:
		return m.mapTurnCompleted(event.Usage)
	case EventTurnFailed:
		return m.mapTurnFailed()
	case EventError:
		return m.mapError(event.Message)
	}
	return nil
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
	case "item/agentMessage/delta":
		return m.mapNotifDelta(params)
	case "turn/completed":
		return m.mapNotifTurnCompleted()
	case "turn/failed":
		return m.mapTurnFailed()
	case "serverRequest/approval",
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval":
		return m.mapNotifApproval(params)
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
	case "warning":
		return m.mapNotifWarning(params)
	case "turn/started":
		m.lastUsage = nil
		m.turnID = extractTurnID(params)
		return nil
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
				Name: "shell",
				Input: map[string]any{
					"command": item.Command,
					"cwd":     item.CWD,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case ItemFileChange:
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   item.ID,
				Name: "file_edit",
				Input: map[string]any{
					"changes": item.Changes,
				},
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
				Name:  "mcp:" + item.Tool,
				Input: args,
			}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

func (m *Mapper) mapItemCompleted(item *CodexItem) []*events.Envelope {
	if item == nil {
		return nil
	}
	switch item.Type {
	case ItemAgentMessage:
		return []*events.Envelope{
			newEnvelope(events.MessageDelta, events.MessageDeltaData{
				MessageID: aep.NewID(),
				Content:   item.Text,
			}, m.sessionID, m.nextSeq()),
		}
	case ItemReasoning:
		return []*events.Envelope{
			newEnvelope(events.Reasoning, events.ReasoningData{
				ID:      item.ID,
				Content: strings.Join(item.SummaryText, "\n"),
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
	case ItemImageGeneration:
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: item.SavedPath,
			}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

func (m *Mapper) mapTurnCompleted(usage *CodexUsage) []*events.Envelope {
	return []*events.Envelope{
		newEnvelope(events.Done, events.DoneData{
			Success: true,
			Stats:   buildUsageStats(usage),
		}, m.sessionID, m.nextSeq()),
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

func (m *Mapper) mapError(msg string) []*events.Envelope {
	return []*events.Envelope{
		newEnvelope(events.Error, events.ErrorData{
			Code: "CODEX_ERROR", Message: msg,
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
	var p struct {
		RequestID string `json:"requestId"`
		ToolName  string `json:"toolName"`
		Reason    string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	desc := p.ToolName
	if p.Reason != "" {
		desc = p.Reason
	}

	return []*events.Envelope{
		newEnvelope(events.PermissionRequest, events.PermissionRequestData{
			ID:          p.RequestID,
			ToolName:    p.ToolName,
			Description: desc,
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

func (m *Mapper) trackTokenUsage(params json.RawMessage) {
	var p struct {
		TurnID    string `json:"turnId"`
		TokenUsage struct {
			Last *CodexTokenUsage `json:"last"`
		} `json:"tokenUsage"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	// Only accept usage for the current turn to prevent stale data.
	if m.turnID != "" && p.TurnID != m.turnID {
		return
	}
	if p.TokenUsage.Last != nil {
		m.lastUsage = p.TokenUsage.Last
	}
}

func (m *Mapper) trackModelRerouted(params json.RawMessage) {
	var p struct {
		ToModel string `json:"toModel"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	m.model = p.ToModel
}

	func (m *Mapper) trackedUsageStats() map[string]any {
	if m.lastUsage == nil && m.model == "" {
		return nil
	}
	stats := make(map[string]any)
	if m.lastUsage != nil {
		usage := map[string]any{
			"input_tokens":  int64(m.lastUsage.InputTokens),
			"output_tokens": int64(m.lastUsage.OutputTokens),
		}
		if m.lastUsage.CachedInputTokens > 0 {
			usage["cache_read_input_tokens"] = int64(m.lastUsage.CachedInputTokens)
		}
		stats["usage"] = usage
	}
	if m.model != "" {
		stats["model_usage"] = map[string]any{
			m.model: map[string]any{},
		}
	}
	return stats
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (m *Mapper) nextSeq() int64 {
	return m.seq.Add(1)
}

// Reset clears internal tracking state, called on session end or crash recovery.
func (m *Mapper) Reset() {
	m.tracker.Reset()
	m.lastUsage = nil
	m.model = ""
	m.turnID = ""
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

// buildUsageStats builds DoneData.Stats from exec-mode CodexUsage.
// Uses nested "usage" format compatible with sessionAccumulator.mergePerTurnStats().
func buildUsageStats(usage *CodexUsage) map[string]any {
	if usage == nil {
		return nil
	}
	usageMap := map[string]any{
		"input_tokens":  int64(usage.InputTokens),
		"output_tokens": int64(usage.OutputTokens),
	}
	if usage.CachedInputTokens > 0 {
		usageMap["cache_read_input_tokens"] = int64(usage.CachedInputTokens)
	}
	return map[string]any{
		"usage": usageMap,
	}
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
