// Package diag provides self-diagnostic capabilities for HotPlex.
// It automatically captures and analyzes runtime errors, generating
// diagnostic reports and optionally creating GitHub issues.
package diag

import (
	"time"
)

// DiagTrigger indicates how the diagnosis was triggered.
type DiagTrigger string

const (
	// TriggerAuto indicates automatic diagnosis from error hooks.
	TriggerAuto DiagTrigger = "auto"
	// TriggerCommand indicates user-initiated diagnosis via slash command.
	TriggerCommand DiagTrigger = "command"
)

// ErrorType categorizes the type of error that occurred.
type ErrorType string

const (
	// ErrorTypeExit indicates CLI process exited abnormally.
	ErrorTypeExit ErrorType = "exit"
	// ErrorTypeTimeout indicates a session timeout.
	ErrorTypeTimeout ErrorType = "timeout"
	// ErrorTypeWAF indicates a security/WAF violation.
	ErrorTypeWAF ErrorType = "waf_violation"
	// ErrorTypePanic indicates a panic or crash.
	ErrorTypePanic ErrorType = "panic"
	// ErrorTypeUnknown indicates an unclassified error.
	ErrorTypeUnknown ErrorType = "unknown"
)

// ErrorInfo contains details about the error that triggered diagnosis.
type ErrorInfo struct {
	// Type categorizes the error.
	Type ErrorType `json:"type"`
	// Message is the human-readable error message.
	Message string `json:"message"`
	// ExitCode is the process exit code (if applicable).
	ExitCode int `json:"exit_code,omitempty"`
	// StackTrace contains the stack trace (if available).
	StackTrace string `json:"stack_trace,omitempty"`
	// Timestamp is when the error occurred.
	Timestamp time.Time `json:"timestamp"`
	// Context provides additional error context.
	Context map[string]any `json:"context,omitempty"`
}

// ConversationData represents the conversation history for diagnosis.
type ConversationData struct {
	// RawSize is the original size in bytes before processing.
	RawSize int `json:"raw_size"`
	// Processed is the processed/summarized conversation content.
	Processed string `json:"processed"`
	// IsSummarized indicates if the content was summarized by LLM.
	IsSummarized bool `json:"is_summarized"`
	// MessageCount is the total number of messages in the conversation.
	MessageCount int `json:"message_count"`
}

// EnvInfo contains environment information for diagnosis.
type EnvInfo struct {
	// HotPlexVersion is the current HotPlex version.
	HotPlexVersion string `json:"hotplex_version"`
	// GoVersion is the Go runtime version.
	GoVersion string `json:"go_version"`
	// OS is the operating system.
	OS string `json:"os"`
	// Arch is the system architecture.
	Arch string `json:"arch"`
	// CLIVersion is the CLI provider version (Claude Code, OpenCode, etc.).
	CLIVersion string `json:"cli_version,omitempty"`
	// ConfigHash is a hash of the configuration (sanitized).
	ConfigHash string `json:"config_hash,omitempty"`
	// Uptime is how long the process has been running.
	Uptime time.Duration `json:"uptime"`
}

// DiagContext contains all information needed for diagnosis.
type DiagContext struct {
	// OriginalSessionID is the ID of the session that encountered the error.
	OriginalSessionID string `json:"original_session_id"`
	// Platform is the chat platform (slack, telegram, dingtalk).
	Platform string `json:"platform"`
	// UserID is the user who triggered the error.
	UserID string `json:"user_id"`
	// ChannelID is the channel where the error occurred.
	ChannelID string `json:"channel_id"`
	// ThreadID is the thread timestamp (for Slack).
	ThreadID string `json:"thread_id,omitempty"`
	// Trigger indicates how the diagnosis was initiated.
	Trigger DiagTrigger `json:"trigger"`
	// Error contains error details.
	Error *ErrorInfo `json:"error"`
	// Conversation contains the conversation history.
	Conversation *ConversationData `json:"conversation"`
	// Logs contains recent log entries (sanitized).
	Logs []byte `json:"logs"`
	// Environment contains system environment info.
	Environment *EnvInfo `json:"environment"`
	// Timestamp is when the diagnosis was created.
	Timestamp time.Time `json:"timestamp"`
}

// IssuePreview represents a GitHub issue preview before creation.
type IssuePreview struct {
	// Title is the auto-generated issue title.
	Title string `json:"title"`
	// Labels are auto-classified labels.
	Labels []string `json:"labels"`
	// Priority is the estimated priority (high/medium/low).
	Priority string `json:"priority"`
	// Summary is a brief problem summary.
	Summary string `json:"summary"`
	// Reproduction contains reproduction steps.
	Reproduction string `json:"reproduction"`
	// Expected is the expected behavior.
	Expected string `json:"expected"`
	// Actual is the actual behavior.
	Actual string `json:"actual"`
	// SuggestedFix is an optional suggested fix.
	SuggestedFix string `json:"suggested_fix,omitempty"`
	// RootCause is the identified root cause (if any).
	RootCause string `json:"root_cause,omitempty"`
}

// DiagResult is the complete diagnosis result.
type DiagResult struct {
	// ID is a unique identifier for this diagnosis.
	ID string `json:"id"`
	// Context is the original diagnosis context.
	Context *DiagContext `json:"context"`
	// Preview is the generated issue preview.
	Preview *IssuePreview `json:"preview"`
	// IssueURL is the URL of the created GitHub issue (if created).
	IssueURL string `json:"issue_url,omitempty"`
	// IssueNumber is the GitHub issue number (if created).
	IssueNumber int `json:"issue_number,omitempty"`
	// Status indicates the current status of the diagnosis.
	Status DiagStatus `json:"status"`
	// CreatedAt is when the diagnosis was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the diagnosis was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// DiagStatus represents the status of a diagnosis.
type DiagStatus string

const (
	// StatusPending indicates diagnosis is waiting for processing.
	StatusPending DiagStatus = "pending"
	// StatusAnalyzing indicates diagnosis is being analyzed by LLM.
	StatusAnalyzing DiagStatus = "analyzing"
	// StatusAwaitingConfirmation indicates waiting for user confirmation.
	StatusAwaitingConfirmation DiagStatus = "awaiting_confirmation"
	// StatusIssueCreated indicates a GitHub issue was created.
	StatusIssueCreated DiagStatus = "issue_created"
	// StatusIgnored indicates the diagnosis was ignored by user.
	StatusIgnored DiagStatus = "ignored"
	// StatusFailed indicates diagnosis failed.
	StatusFailed DiagStatus = "failed"
)

// Config contains diagnostic configuration.
type Config struct {
	// Enabled indicates if diagnostics are enabled.
	Enabled bool `json:"enabled"`
	// NotifyChannel is the fixed channel for diagnostic notifications.
	NotifyChannel string `json:"notify_channel"`
	// LogSizeLimit is the maximum log size in bytes (default 20KB).
	LogSizeLimit int `json:"log_size_limit"`
	// ConversationSizeLimit is the maximum conversation size in bytes (default 20KB).
	ConversationSizeLimit int `json:"conversation_size_limit"`
	// ConfirmTimeout is the timeout for user confirmation (default 5min).
	ConfirmTimeout time.Duration `json:"confirm_timeout"`
	// GitHub configuration.
	GitHub GitHubConfig `json:"github"`
}

// GitHubConfig contains GitHub-related configuration.
type GitHubConfig struct {
	// Repo is the target repository (e.g., "hrygo/hotplex").
	Repo string `json:"repo"`
	// Labels are default labels for created issues.
	Labels []string `json:"labels"`
	// Token is the GitHub API token (from secrets provider).
	Token string `json:"-"`
}

// DefaultConfig returns the default diagnostic configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:               true,
		LogSizeLimit:          20 * 1024,       // 20KB
		ConversationSizeLimit: 20 * 1024,       // 20KB
		ConfirmTimeout:        5 * time.Minute, // 5 minutes
		GitHub: GitHubConfig{
			Repo:   "hrygo/hotplex",
			Labels: []string{"bug", "auto-diagnosed"},
		},
	}
}

// Trigger is the interface for diagnosis triggers.
type Trigger interface {
	// Type returns the trigger type.
	Type() DiagTrigger
	// SessionID returns the associated session ID.
	SessionID() string
	// Error returns the error information.
	Error() *ErrorInfo
	// Platform returns the chat platform.
	Platform() string
	// UserID returns the user ID.
	UserID() string
	// ChannelID returns the channel ID.
	ChannelID() string
	// ThreadID returns the thread ID (for Slack).
	ThreadID() string
}

// BaseTrigger provides a basic implementation of Trigger.
type BaseTrigger struct {
	triggerType   DiagTrigger
	sessionID     string
	err           *ErrorInfo
	platform      string
	userID        string
	channelID     string
	threadID      string
}

// NewBaseTrigger creates a new BaseTrigger.
func NewBaseTrigger(triggerType DiagTrigger, sessionID string, err *ErrorInfo) *BaseTrigger {
	return &BaseTrigger{
		triggerType: triggerType,
		sessionID:   sessionID,
		err:         err,
	}
}

func (t *BaseTrigger) Type() DiagTrigger    { return t.triggerType }
func (t *BaseTrigger) SessionID() string    { return t.sessionID }
func (t *BaseTrigger) Error() *ErrorInfo    { return t.err }
func (t *BaseTrigger) Platform() string     { return t.platform }
func (t *BaseTrigger) UserID() string       { return t.userID }
func (t *BaseTrigger) ChannelID() string    { return t.channelID }
func (t *BaseTrigger) ThreadID() string     { return t.threadID }

// SetPlatform sets the platform.
func (t *BaseTrigger) SetPlatform(p string) *BaseTrigger {
	t.platform = p
	return t
}

// SetUserID sets the user ID.
func (t *BaseTrigger) SetUserID(u string) *BaseTrigger {
	t.userID = u
	return t
}

// SetChannelID sets the channel ID.
func (t *BaseTrigger) SetChannelID(c string) *BaseTrigger {
	t.channelID = c
	return t
}

// SetThreadID sets the thread ID.
func (t *BaseTrigger) SetThreadID(tid string) *BaseTrigger {
	t.threadID = tid
	return t
}
