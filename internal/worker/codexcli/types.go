package codexcli

import (
	"encoding/json"
	"fmt"
)

// CodexEvent is a Codex JSONL event.
type CodexEvent struct {
	Type    string     `json:"type"`
	Item    *CodexItem `json:"item,omitempty"`
	Message string     `json:"message,omitempty"`
}

// CodexItem represents a single turn item within a codex turn.
type CodexItem struct {
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	Text        string                     `json:"text,omitempty"`
	SummaryText []string                   `json:"summary_text,omitempty"`
	RawContent  []string                   `json:"raw_content,omitempty"`
	Command     string                     `json:"command,omitempty"`
	CWD         string                     `json:"cwd,omitempty"`
	Stdout      string                     `json:"stdout,omitempty"`
	Stderr      string                     `json:"stderr,omitempty"`
	ExitCode    int                        `json:"exit_code,omitempty"`
	Changes     map[string]CodexFileChange `json:"changes,omitempty"`
	Status      string                     `json:"status,omitempty"`
	Server      string                     `json:"server,omitempty"`
	Tool        string                     `json:"tool,omitempty"`
	Arguments   json.RawMessage            `json:"arguments,omitempty"`
	Result      json.RawMessage            `json:"result,omitempty"`
	Error       *CodexItemError            `json:"error,omitempty"`
	Duration    int64                      `json:"duration,omitempty"`
	Query       string                     `json:"query,omitempty"`
	Action      string                     `json:"action,omitempty"`
	Phase       string                     `json:"phase,omitempty"`
	// CollabToolCall fields
	CollabTool string   `json:"collab_tool,omitempty"`
	Agents     []string `json:"agents,omitempty"`
	// WebSearch fields
	Results json.RawMessage `json:"results,omitempty"`
	// TodoList fields
	TodoItems []CodexTodoItem `json:"todo_items,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"` // generic payload for error items
}

// CodexTokenUsage holds per-turn or cumulative token usage from app-server
// thread/tokenUsage/updated notifications (TokenUsageBreakdown in codex schema).
type CodexTokenUsage struct {
	TotalTokens           int `json:"totalTokens"`
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
}

// CodexFileChange describes a single file modification.
type CodexFileChange struct {
	FilePath string `json:"file_path,omitempty"`
}

// CodexTodoItem represents a single todo item within a TodoList.
type CodexTodoItem struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Priority    string `json:"priority,omitempty"`
}

// CodexItemError represents an error within an item.
type CodexItemError struct {
	Message string `json:"message"`
}

// Event type constants for protocol dispatch.
const (
	ContentTypeText = "text"
)

const (
	EventThreadStarted = "thread.started"
	EventTurnStarted   = "turn.started"
	EventTurnCompleted = "turn.completed"
	EventTurnFailed    = "turn.failed"
	EventItemStarted   = "item.started"
	EventItemUpdated   = "item.updated"
	EventItemCompleted = "item.completed"
	EventError         = "error"
)

// Item type constants used in CodexItem.Type and mapper switch cases.
const (
	ItemCommandExecution = "command_execution"
	ItemFileChange       = "file_change"
	ItemMCPToolCall      = "mcp_tool_call"
	ItemAgentMessage     = "agent_message"
	ItemReasoning        = "reasoning"
	ItemPlan             = "plan"
	ItemCollabToolCall   = "collab_tool_call"
	ItemWebSearch        = "web_search"
	ItemTodoList         = "todo_list"
	ItemError            = "error"
)

// EnvBlocklist defines environment variable prefixes to strip from worker processes.
var EnvBlocklist = []string{"HOTPLEX_", "CODEX_", "CODEXCLI"}

// ─── JSON-RPC 2.0 Wire Types (app-server mode) ──────────────────────────

// JSONRPCFrame is a unified type for single-pass frame parsing. It captures all
// routing fields (ID, Method, Error) so dispatchFrame can branch without a
// second unmarshal.
type JSONRPCFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JSONRPCID       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCID preserves a JSON-RPC request ID exactly as received. Codex uses
// both integer and string IDs, and integer zero is a valid first server-request
// ID. A zero-length value means the id field was absent.
type JSONRPCID json.RawMessage

func (id *JSONRPCID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = nil
		return nil
	}
	var integer int64
	if err := json.Unmarshal(data, &integer); err == nil {
		*id = append((*id)[:0], data...)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*id = append((*id)[:0], data...)
		return nil
	}
	return fmt.Errorf("codex-app-server: invalid JSON-RPC id: %s", data)
}

func (id JSONRPCID) MarshalJSON() ([]byte, error) {
	if len(id) == 0 {
		return []byte("null"), nil
	}
	return id, nil
}

func (id JSONRPCID) IsSet() bool { return len(id) > 0 }

func (id JSONRPCID) Int64() (int64, bool) {
	var value int64
	if len(id) == 0 || json.Unmarshal(id, &value) != nil {
		return 0, false
	}
	return value, true
}

func (id JSONRPCID) Key() string {
	if len(id) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(id, &text) == nil {
		return text
	}
	return string(id)
}

func (id JSONRPCID) Clone() JSONRPCID {
	return append(JSONRPCID(nil), id...)
}

func integerJSONRPCID(id int64) JSONRPCID {
	return JSONRPCID(json.RawMessage(fmt.Sprintf("%d", id)))
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCServerResponse is used only when answering a server-initiated
// request. Unlike client responses, its ID must be echoed without coercion.
type JSONRPCServerResponse struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      JSONRPCID       `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// App-server specific param/response payloads.

type ThreadStartParams struct {
	Model          string `json:"model,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	Personality    string `json:"personality,omitempty"`
	Ephemeral      bool   `json:"ephemeral,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"` // "never" (YOLO) | "on-request" | "on-failure" | "untrusted"

	// Agent-configs injection. BaseInstructions carries the merged B/C channel
	// system prompt from bridge.injectAgentConfig(). Mapped to the app-server
	// thread/start "baseInstructions" field (ThreadStartParams.base_instructions
	// in Rust). DeveloperInstructions is reserved for future use — higher
	// priority than BaseInstructions when both are set.
	BaseInstructions      string `json:"baseInstructions,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type ThreadStartResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type TurnStartParams struct {
	ThreadID string          `json:"threadId"`
	Input    []TurnInputItem `json:"input"`
}

type TurnInputItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type ThreadUnsubscribeParams struct {
	ThreadID string `json:"threadId"`
}

type ThreadUnsubscribeResult struct {
	Status string `json:"status"`
}

// SkillsListParams is the app-server `skills/list` request. When Cwds is empty
// the server defaults to the current session working directory.
type SkillsListParams struct {
	Cwds        []string `json:"cwds,omitempty"`
	ForceReload bool     `json:"forceReload,omitempty"`
}

// SkillsListResponse is the app-server `skills/list` response, one entry per
// requested cwd.
type SkillsListResponse struct {
	Data []SkillsListEntry `json:"data"`
}

// SkillsListEntry groups the skills discovered under a single cwd.
type SkillsListEntry struct {
	Cwd    string           `json:"cwd"`
	Errors []SkillErrorInfo `json:"errors"`
	Skills []SkillMetadata  `json:"skills"`
}

// SkillErrorInfo reports a per-cwd scan failure without aborting the list.
type SkillErrorInfo struct {
	Message string `json:"message,omitempty"`
}

// SkillMetadata is the authoritative description of a skill as resolved by
// Codex for a given cwd (SKILL.md / SKILL.json).
type SkillMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
}
