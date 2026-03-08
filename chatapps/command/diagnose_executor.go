package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/internal/diag"
	"github.com/hrygo/hotplex/provider"
)

// CommandDiagnose represents the /diagnose command
const CommandDiagnose = "/diagnose"

// DiagnoseExecutor executes the /diagnose command
type DiagnoseExecutor struct {
	diagnostician diag.DiagnosticianInterface
	issueCreator  *diag.IssueCreator
	config        *diag.Config
	logger        *slog.Logger
}

// Compile-time interface compliance check
var _ Executor = (*DiagnoseExecutor)(nil)

// NewDiagnoseExecutor creates a new DiagnoseExecutor
func NewDiagnoseExecutor(
	diagnostician diag.DiagnosticianInterface,
	config *diag.Config,
	logger *slog.Logger,
) *DiagnoseExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &DiagnoseExecutor{
		diagnostician: diagnostician,
		issueCreator:  diag.NewIssueCreator(config, logger),
		config:        config,
		logger:        logger.With("component", "command.diagnose"),
	}
}

// Command returns the command name
func (e *DiagnoseExecutor) Command() string {
	return CommandDiagnose
}

// Description returns a human-readable description
func (e *DiagnoseExecutor) Description() string {
	return "Run diagnostics on the current session and create a GitHub issue"
}

// Execute runs the /diagnose command
func (e *DiagnoseExecutor) Execute(ctx context.Context, req *Request, callback event.Callback) (*Result, error) {
	e.logger.Info("Diagnose command invoked",
		"user", req.UserID,
		"channel", req.ChannelID,
		"session", req.SessionID,
	)

	// Check if diagnostics is enabled
	if !e.config.Enabled {
		return &Result{
			Success: false,
			Message: "Diagnostics is not enabled",
		}, nil
	}

	// Check if command is allowed in this channel
	if e.config.NotifyChannel != "" && req.ChannelID != e.config.NotifyChannel {
		return &Result{
			Success: false,
			Message: "The /diagnose command is only available in the designated diagnostics channel",
		}, nil
	}

	// Define progress steps
	steps := []ProgressStep{
		{Name: "collect", Message: "Collecting diagnostic context...", Status: "pending"},
		{Name: "analyze", Message: "Analyzing error...", Status: "pending"},
		{Name: "create_issue", Message: "Creating GitHub issue...", Status: "pending"},
	}
	emitter := NewProgressEmitter(e.Command(), callback, steps)

	// Step 1: Collect context
	_ = emitter.Running(0)

	// Create trigger from request
	trigger := &diagSessionTrigger{
		sessionID: req.SessionID,
		userID:    req.UserID,
		channelID: req.ChannelID,
		threadID:  req.ThreadTS,
		platform:  extractPlatform(req.Metadata),
	}

	// Step 2: Run diagnosis
	_ = emitter.Success(0, "Context collected")
	_ = emitter.Running(1)

	result, err := e.diagnostician.Diagnose(ctx, trigger)
	if err != nil {
		e.logger.Error("Diagnosis failed", "error", err)
		_ = emitter.Error(1, fmt.Sprintf("Diagnosis failed: %v", err))
		_ = emitter.Emit("Diagnosis Failed")
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Diagnosis failed: %v", err),
		}, nil
	}

	// Step 3: Create GitHub issue
	_ = emitter.Success(1, "Analysis complete")
	_ = emitter.Running(2)

	issueResult, err := e.issueCreator.Create(ctx, result)
	if err != nil {
		e.logger.Error("Issue creation failed", "error", err)
		_ = emitter.Error(2, fmt.Sprintf("Issue creation failed: %v", err))
		_ = emitter.Emit("Issue Creation Failed")
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Issue creation failed: %v", err),
		}, nil
	}

	// Update result status
	result.Status = diag.StatusIssueCreated
	result.IssueURL = issueResult.URL
	result.IssueNumber = issueResult.Number

	_ = emitter.Success(2, "Issue created")
	_ = emitter.Emit("Diagnosis Complete")
	_ = emitter.Complete(buildSuccessMessage(result, issueResult))

	e.logger.Info("Diagnosis complete, issue created",
		"issue_url", issueResult.URL,
		"issue_number", issueResult.Number,
	)

	return &Result{
		Success: true,
		Message: buildSuccessMessage(result, issueResult),
		Metadata: map[string]any{
			"issue_url":    issueResult.URL,
			"issue_number": issueResult.Number,
			"diag_id":      result.ID,
		},
	}, nil
}

// buildSuccessMessage builds the success message
func buildSuccessMessage(result *diag.DiagResult, issueResult *diag.IssueCreationResult) string {
	message := "✅ Diagnosis complete!\n\n"
	if result.Preview != nil {
		message += fmt.Sprintf("**%s**\n", result.Preview.Title)
		message += fmt.Sprintf("Priority: %s\n", result.Preview.Priority)
		message += fmt.Sprintf("Summary: %s\n\n", result.Preview.Summary)
	}
	message += fmt.Sprintf("GitHub Issue: %s", issueResult.URL)
	return message
}

// extractPlatform extracts platform from metadata
func extractPlatform(metadata map[string]any) string {
	if metadata == nil {
		return "slack"
	}
	if platform, ok := metadata["platform"].(string); ok {
		return platform
	}
	return "slack"
}

// diagSessionTrigger implements diag.Trigger for existing sessions
type diagSessionTrigger struct {
	sessionID string
	userID    string
	channelID string
	threadID  string
	platform  string
	err       *diag.ErrorInfo
}

func (t *diagSessionTrigger) Type() diag.DiagTrigger {
	return diag.TriggerCommand
}

func (t *diagSessionTrigger) SessionID() string {
	return t.sessionID
}

func (t *diagSessionTrigger) Error() *diag.ErrorInfo {
	if t.err == nil {
		return &diag.ErrorInfo{
			Type:      diag.ErrorTypeUnknown,
			Message:   "Manual diagnosis triggered by user",
			Timestamp: time.Now(),
		}
	}
	return t.err
}

func (t *diagSessionTrigger) Platform() string {
	return t.platform
}

func (t *diagSessionTrigger) UserID() string {
	return t.userID
}

func (t *diagSessionTrigger) ChannelID() string {
	return t.channelID
}

func (t *diagSessionTrigger) ThreadID() string {
	return t.threadID
}

// WithError sets an error for the trigger
func (t *diagSessionTrigger) WithError(err *diag.ErrorInfo) *diagSessionTrigger {
	t.err = err
	return t
}

// EmitCommandEvent is a helper to emit command events
func EmitCommandEvent(callback event.Callback, eventType string, message string, meta *event.EventMeta) error {
	if callback == nil {
		return nil
	}
	return callback(string(provider.EventTypeCommandProgress), event.NewEventWithMeta(eventType, message, meta))
}
