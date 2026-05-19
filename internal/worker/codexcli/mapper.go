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
		return m.mapNotifTurnCompleted(params)
	case "turn/failed":
		return m.mapTurnFailed()
	case "serverRequest/approval":
		return m.mapNotifApproval(params)
	case "thread/started":
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
		ItemID    string `json:"itemId"`
		TextDelta string `json:"textDelta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.MessageDelta, events.MessageDeltaData{
			MessageID: m.tracker.getMessageID(p.ItemID),
			Content:   p.TextDelta,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifTurnCompleted(params json.RawMessage) []*events.Envelope {
	var p struct {
		Turn struct {
			Usage *CodexUsage `json:"usage,omitempty"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.Done, events.DoneData{
			Success: true,
			Stats:   buildUsageStats(p.Turn.Usage),
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

// ─── Helpers ─────────────────────────────────────────────────────────────

func (m *Mapper) nextSeq() int64 {
	return m.seq.Add(1)
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

func buildUsageStats(usage *CodexUsage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
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
