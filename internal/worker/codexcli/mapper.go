package codexcli

import (
	"encoding/json"
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

func (m *Mapper) Map(event *CodexEvent) []*events.Envelope {
	switch event.Type {
	case EventItemStarted:
		return m.mapItemStarted(event.Item)
	case EventItemCompleted:
		return m.mapItemCompleted(event.Item)
	case EventTurnCompleted:
		return m.mapTurnCompleted(event.Usage)
	case EventTurnFailed:
		return []*events.Envelope{
			newEnvelope(events.Error, events.ErrorData{
				Code: "TURN_FAILED", Message: "turn failed",
			}, m.sessionID, m.nextSeq()),
			newEnvelope(events.Done, events.DoneData{Success: false}, m.sessionID, m.nextSeq()),
		}
	case EventError:
		return []*events.Envelope{
			newEnvelope(events.Error, events.ErrorData{
				Code: "CODEX_ERROR", Message: event.Message,
			}, m.sessionID, m.nextSeq()),
			newEnvelope(events.Done, events.DoneData{Success: false}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

func (m *Mapper) mapItemStarted(item *CodexItem) []*events.Envelope {
	if item == nil {
		return nil
	}
	switch item.Type {
	case "command_execution":
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
	case "file_change":
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   item.ID,
				Name: "file_edit",
				Input: map[string]any{
					"changes": item.Changes,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case "mcp_tool_call":
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
	case "agent_message":
		return []*events.Envelope{
			newEnvelope(events.MessageDelta, events.MessageDeltaData{
				MessageID: aep.NewID(),
				Content:   item.Text,
			}, m.sessionID, m.nextSeq()),
		}
	case "reasoning":
		content := ""
		if len(item.SummaryText) > 0 {
			for i, s := range item.SummaryText {
				if i > 0 {
					content += "\n"
				}
				content += s
			}
		}
		return []*events.Envelope{
			newEnvelope(events.Reasoning, events.ReasoningData{
				Content: content,
			}, m.sessionID, m.nextSeq()),
		}
	case "command_execution":
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: item.Stdout,
				Error:  item.Stderr,
			}, m.sessionID, m.nextSeq()),
		}
	case "file_change":
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
	case "mcp_tool_call":
		resultOutput := string(item.Result)
		var errMsg string
		if item.Error != nil {
			errMsg = item.Error.Message
		}
		return []*events.Envelope{
			newEnvelope(events.ToolResult, events.ToolResultData{
				ID:     item.ID,
				Output: resultOutput,
				Error:  errMsg,
			}, m.sessionID, m.nextSeq()),
		}
	case "plan":
		return []*events.Envelope{
			newEnvelope(events.State, events.StateData{
				State:   "planning",
				Message: item.Text,
			}, m.sessionID, m.nextSeq()),
		}
	case "image_generation":
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
	stats := map[string]any{}
	if usage != nil {
		stats["input_tokens"] = usage.InputTokens
		stats["output_tokens"] = usage.OutputTokens
	}
	return []*events.Envelope{
		newEnvelope(events.Done, events.DoneData{
			Success: true,
			Stats:   stats,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) nextSeq() int64 {
	return m.seq.Add(1)
}

func newEnvelope(kind events.Kind, data interface{}, sessionID string, seq int64) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, seq, kind, data)
}

// ─── MapNotification (v2 app-server mode) ──────────────────────────────

func (m *Mapper) MapNotification(method string, params json.RawMessage) []*events.Envelope {
	switch method {
	case "item/started":
		return m.mapNotifItemStarted(params)
	case "item/completed":
		return m.mapNotifItemCompleted(params)
	case "item/agentMessage/delta":
		return m.mapNotifDelta(params)
	case "turn/completed":
		return m.mapNotifTurnCompleted(params)
	case "turn/failed":
		return []*events.Envelope{
			newEnvelope(events.Error, events.ErrorData{
				Code: "TURN_FAILED", Message: "turn failed",
			}, m.sessionID, m.nextSeq()),
			newEnvelope(events.Done, events.DoneData{Success: false}, m.sessionID, m.nextSeq()),
		}
	case "serverRequest/approval":
		return m.mapNotifApproval(params)
	case "thread/started":
		return nil // informational, no AEP mapping needed
	}
	return nil
}

func (m *Mapper) mapNotifItemStarted(params json.RawMessage) []*events.Envelope {
	var p struct {
		Item struct {
			ID      string                     `json:"id"`
			Type    string                     `json:"type"`
			Command string                     `json:"command,omitempty"`
			CWD     string                     `json:"cwd,omitempty"`
			Changes map[string]CodexFileChange `json:"changes,omitempty"`
			Server  string                     `json:"server,omitempty"`
			Tool    string                     `json:"tool,omitempty"`
		} `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}

	switch p.Item.Type {
	case "agent_message":
		m.tracker.startMessage(p.Item.ID)
		return []*events.Envelope{
			newEnvelope(events.MessageStart, events.MessageStartData{
				ID:          m.tracker.getMessageID(p.Item.ID),
				Role:        "assistant",
				ContentType: ContentTypeText,
			}, m.sessionID, m.nextSeq()),
		}
	case "command_execution":
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   p.Item.ID,
				Name: "shell",
				Input: map[string]any{
					"command": p.Item.Command,
					"cwd":     p.Item.CWD,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case "file_change":
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   p.Item.ID,
				Name: "file_edit",
				Input: map[string]any{
					"changes": p.Item.Changes,
				},
			}, m.sessionID, m.nextSeq()),
		}
	case "mcp_tool_call":
		return []*events.Envelope{
			newEnvelope(events.ToolCall, events.ToolCallData{
				ID:   p.Item.ID,
				Name: "mcp:" + p.Item.Tool,
			}, m.sessionID, m.nextSeq()),
		}
	}
	return nil
}

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

func (m *Mapper) mapNotifItemCompleted(params json.RawMessage) []*events.Envelope {
	var p struct {
		Item struct {
			ID          string          `json:"id"`
			Type        string          `json:"type"`
			Text        string          `json:"text,omitempty"`
			SummaryText []string        `json:"summary_text,omitempty"`
			Stdout      string          `json:"stdout,omitempty"`
			Stderr      string          `json:"stderr,omitempty"`
			Status      string          `json:"status,omitempty"`
			Result      json.RawMessage `json:"result,omitempty"`
			SavedPath   string          `json:"saved_path,omitempty"`
		} `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}

	var envs []*events.Envelope

	switch p.Item.Type {
	case "agent_message":
		envs = append(envs, newEnvelope(events.MessageEnd, events.MessageEndData{
			MessageID: m.tracker.getMessageID(p.Item.ID),
		}, m.sessionID, m.nextSeq()))
		m.tracker.endMessage(p.Item.ID)
	case "reasoning":
		content := ""
		for i, s := range p.Item.SummaryText {
			if i > 0 {
				content += "\n"
			}
			content += s
		}
		envs = append(envs, newEnvelope(events.Reasoning, events.ReasoningData{
			Content: content,
		}, m.sessionID, m.nextSeq()))
	case "command_execution":
		envs = append(envs, newEnvelope(events.ToolResult, events.ToolResultData{
			ID:     p.Item.ID,
			Output: p.Item.Stdout,
			Error:  p.Item.Stderr,
		}, m.sessionID, m.nextSeq()))
	case "file_change":
		status := "completed"
		if p.Item.Status != "completed" {
			status = "failed"
		}
		envs = append(envs, newEnvelope(events.ToolResult, events.ToolResultData{
			ID:     p.Item.ID,
			Output: status,
			Error:  p.Item.Stderr,
		}, m.sessionID, m.nextSeq()))
	case "image_generation":
		envs = append(envs, newEnvelope(events.ToolResult, events.ToolResultData{
			ID:     p.Item.ID,
			Output: p.Item.SavedPath,
		}, m.sessionID, m.nextSeq()))
	}

	return envs
}

func (m *Mapper) mapNotifTurnCompleted(params json.RawMessage) []*events.Envelope {
	var p struct {
		Turn struct {
			Usage *CodexUsage `json:"usage,omitempty"`
		} `json:"turn"`
	}
	json.Unmarshal(params, &p)

	stats := map[string]any{}
	if p.Turn.Usage != nil {
		stats["input_tokens"] = p.Turn.Usage.InputTokens
		stats["output_tokens"] = p.Turn.Usage.OutputTokens
	}
	return []*events.Envelope{
		newEnvelope(events.Done, events.DoneData{
			Success: true,
			Stats:   stats,
		}, m.sessionID, m.nextSeq()),
	}
}

func (m *Mapper) mapNotifApproval(params json.RawMessage) []*events.Envelope {
	var p struct {
		RequestID string `json:"requestId"`
		ToolName  string `json:"toolName"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return []*events.Envelope{
		newEnvelope(events.PermissionRequest, events.PermissionRequestData{
			ID:          p.RequestID,
			ToolName:    p.ToolName,
			Description: p.ToolName,
		}, m.sessionID, m.nextSeq()),
	}
}

// ─── messageTracker ─────────────────────────────────────────────────────

type messageTracker struct {
	mu       sync.Mutex
	messages map[string]*messageState
}

type messageState struct {
	messageID string
}

func newMessageTracker() *messageTracker {
	return &messageTracker{
		messages: make(map[string]*messageState),
	}
}

func (t *messageTracker) startMessage(itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages[itemID] = &messageState{messageID: aep.NewID()}
}

func (t *messageTracker) getMessageID(itemID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.messages[itemID]; ok {
		return s.messageID
	}
	// Lazy-create if missing (delta arrived before item/started)
	id := aep.NewID()
	t.messages[itemID] = &messageState{messageID: id}
	return id
}

func (t *messageTracker) endMessage(itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.messages, itemID)
}
