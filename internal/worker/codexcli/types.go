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
	SavedPath   string                     `json:"saved_path,omitempty"`
	Phase       string                     `json:"phase,omitempty"`
}

// CodexUsage holds token usage statistics from turn.completed.
type CodexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CodexFileChange describes a single file modification.
type CodexFileChange struct {
	FilePath string `json:"file_path,omitempty"`
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
	ItemImageGeneration  = "image_generation"
)

// EnvBlocklist defines environment variable prefixes to strip from worker processes.
var EnvBlocklist = []string{"HOTPLEX_", "CODEX_"}

// ─── JSON-RPC 2.0 Wire Types (app-server mode) ──────────────────────────

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
	Model       string `json:"model,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	Sandbox     string `json:"sandbox,omitempty"`
	Personality string `json:"personality,omitempty"`
	Ephemeral   bool   `json:"ephemeral,omitempty"`
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
