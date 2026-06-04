package codexcli

import "encoding/json"

// CodexEvent is the top-level JSONL event from codex exec --json.
type CodexEvent struct {
	Type     string      `json:"type"`
	Item     *CodexItem  `json:"item,omitempty"`
	ThreadID string      `json:"thread_id,omitempty"`
	Usage    *CodexUsage `json:"usage,omitempty"`
	Message  string      `json:"message,omitempty"`
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

// CodexUsage holds token usage statistics from turn.completed (exec mode).
// Fields use camelCase to match codex exec serde serialization.
type CodexUsage struct {
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
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
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
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
	ApprovalPolicy string `json:"approvalPolicy,omitempty"` // "never" | "on-request" | "on-failure" | "untrusted"
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
	Text string `json:"text"`
}

type ThreadUnsubscribeParams struct {
	ThreadID string `json:"threadId"`
}

type ThreadUnsubscribeResult struct {
	Status string `json:"status"`
}
