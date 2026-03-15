package diag

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"log/slog"

	"github.com/hrygo/hotplex/brain"
	"github.com/hrygo/hotplex/internal/persistence"
)

// Diagnostician is the core diagnostic engine.
type Diagnostician struct {
	config       *Config
	collector    *Collector
	brain        brain.Brain
	historyStore persistence.MessageHistoryStore
	logger       *slog.Logger

	// Track pending diagnoses awaiting confirmation
	pending sync.Map // map[string]*pendingDiag
}

type pendingDiag struct {
	result    *DiagResult
	createdAt time.Time
	timer     *time.Timer
}

// Compile-time interface compliance check
var _ DiagnosticianInterface = (*Diagnostician)(nil)

// DiagnosticianInterface defines the diagnostician contract.
type DiagnosticianInterface interface {
	// Diagnose performs diagnosis and returns a result.
	Diagnose(ctx context.Context, trigger Trigger) (*DiagResult, error)
	// ConfirmIssue confirms creation of a pending issue.
	ConfirmIssue(ctx context.Context, diagID string) (string, error)
	// IgnoreIssue ignores a pending diagnosis.
	IgnoreIssue(ctx context.Context, diagID string) error
	// GetPending returns all pending diagnoses.
	GetPending() []*DiagResult
}

// NewDiagnostician creates a new Diagnostician.
func NewDiagnostician(config *Config, historyStore persistence.MessageHistoryStore, br brain.Brain, logger *slog.Logger) *Diagnostician {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	d := &Diagnostician{
		config:       config,
		historyStore: historyStore,
		brain:        br,
		logger:       logger.With("component", "diag"),
	}

	d.collector = NewCollector(config, historyStore, br)

	return d
}

// Diagnose performs diagnosis and returns a result.
func (d *Diagnostician) Diagnose(ctx context.Context, trigger Trigger) (*DiagResult, error) {
	if !d.config.Enabled {
		return nil, fmt.Errorf("diagnostics disabled")
	}

	d.logger.Info("Starting diagnosis",
		"session_id", trigger.SessionID(),
		"trigger", trigger.Type(),
	)

	// Collect context
	diagCtx, err := d.collector.Collect(ctx, trigger)
	if err != nil {
		return nil, fmt.Errorf("collect context: %w", err)
	}

	// Generate issue preview using Brain
	preview, err := d.generatePreview(ctx, diagCtx)
	if err != nil {
		d.logger.Warn("Failed to generate preview, using fallback", "error", err)
		preview = d.fallbackPreview(diagCtx)
	}

	result := &DiagResult{
		ID:        uuid.New().String(),
		Context:   diagCtx,
		Preview:   preview,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// For command trigger, create issue directly
	if trigger.Type() == TriggerCommand {
		return result, nil
	}

	// For auto trigger, set status to awaiting confirmation
	result.Status = StatusAwaitingConfirmation

	// Store as pending with timeout
	d.storePending(result)

	return result, nil
}

// generatePreview uses Brain to generate an issue preview.
func (d *Diagnostician) generatePreview(ctx context.Context, diagCtx *DiagContext) (*IssuePreview, error) {
	if d.brain == nil {
		return nil, fmt.Errorf("brain not available")
	}

	// Build context for analysis
	contextStr := BuildDiagnosisContext(diagCtx)
	prompt := fmt.Sprintf(DiagnosePrompt, contextStr)

	// Get structured analysis from Brain
	var preview IssuePreview
	err := d.brain.Analyze(ctx, prompt, &preview)
	if err != nil {
		return nil, fmt.Errorf("brain analyze: %w", err)
	}

	// Ensure required fields have defaults
	if preview.Title == "" {
		preview.Title = fmt.Sprintf("[%s] Error in session %s", diagCtx.Error.Type, diagCtx.OriginalSessionID[:8])
	}
	if len(preview.Labels) == 0 {
		preview.Labels = d.config.GitHub.Labels
	}
	if preview.Priority == "" {
		preview.Priority = "medium"
	}

	return &preview, nil
}

// fallbackPreview creates a basic preview when Brain is unavailable.
func (d *Diagnostician) fallbackPreview(diagCtx *DiagContext) *IssuePreview {
	title := fmt.Sprintf("Auto-diagnosed: %s error", diagCtx.Error.Type)
	if diagCtx.Error != nil && diagCtx.Error.Message != "" {
		// Truncate long messages for title
		msg := diagCtx.Error.Message
		if len(msg) > 60 {
			msg = msg[:60] + "..."
		}
		title = fmt.Sprintf("Auto-diagnosed: %s", msg)
	}

	summary := "Automatic diagnosis triggered by error"
	if diagCtx.Error != nil {
		summary = diagCtx.Error.Message
	}

	return &IssuePreview{
		Title:        title,
		Labels:       d.config.GitHub.Labels,
		Priority:     "medium",
		Summary:      summary,
		Reproduction: "Automatically detected - see context for details",
		Expected:     "Normal operation",
		Actual:       diagCtx.Error.Message,
	}
}

// storePending stores a diagnosis as pending with auto-timeout.
func (d *Diagnostician) storePending(result *DiagResult) {
	pd := &pendingDiag{
		result:    result,
		createdAt: time.Now(),
	}

	// Set timeout for auto-creation
	timeout := d.config.ConfirmTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	pd.timer = time.AfterFunc(timeout, func() {
		d.logger.Info("Diagnosis timeout, auto-creating issue", "diag_id", result.ID)
		_, err := d.ConfirmIssue(context.Background(), result.ID)
		if err != nil {
			d.logger.Error("Failed to auto-create issue", "error", err)
		}
	})

	d.pending.Store(result.ID, pd)
}

// ConfirmIssue confirms and creates a GitHub issue for a pending diagnosis.
func (d *Diagnostician) ConfirmIssue(ctx context.Context, diagID string) (string, error) {
	value, ok := d.pending.Load(diagID)
	if !ok {
		return "", fmt.Errorf("diagnosis not found: %s", diagID)
	}

	pd := value.(*pendingDiag)

	// Cancel timeout timer
	if pd.timer != nil {
		pd.timer.Stop()
	}

	// Remove from pending
	d.pending.Delete(diagID)

	// Update status
	pd.result.Status = StatusAnalyzing
	pd.result.UpdatedAt = time.Now()

	// Create GitHub issue
	issueURL, err := d.createGitHubIssue(ctx, pd.result)
	if err != nil {
		pd.result.Status = StatusFailed
		pd.result.UpdatedAt = time.Now()
		return "", fmt.Errorf("create issue: %w", err)
	}

	pd.result.Status = StatusIssueCreated
	pd.result.IssueURL = issueURL
	pd.result.UpdatedAt = time.Now()

	d.logger.Info("Issue created", "diag_id", diagID, "url", issueURL)

	return issueURL, nil
}

// IgnoreIssue ignores a pending diagnosis.
func (d *Diagnostician) IgnoreIssue(ctx context.Context, diagID string) error {
	value, ok := d.pending.Load(diagID)
	if !ok {
		return fmt.Errorf("diagnosis not found: %s", diagID)
	}

	pd := value.(*pendingDiag)

	// Cancel timeout timer
	if pd.timer != nil {
		pd.timer.Stop()
	}

	// Remove from pending
	d.pending.Delete(diagID)

	pd.result.Status = StatusIgnored
	pd.result.UpdatedAt = time.Now()

	d.logger.Info("Diagnosis ignored", "diag_id", diagID)

	return nil
}

// GetPending returns all pending diagnoses.
func (d *Diagnostician) GetPending() []*DiagResult {
	var results []*DiagResult
	d.pending.Range(func(key, value any) bool {
		pd := value.(*pendingDiag)
		results = append(results, pd.result)
		return true
	})
	return results
}

// createGitHubIssue creates a GitHub issue from a diagnosis result.
// This is a placeholder - real implementation would use GitHub API.
func (d *Diagnostician) createGitHubIssue(ctx context.Context, result *DiagResult) (string, error) {
	// Build issue body
	body := d.buildIssueBody(result)

	// TODO: Integrate with GitHub API
	// For now, return a placeholder
	d.logger.Info("Would create GitHub issue",
		"title", result.Preview.Title,
		"labels", result.Preview.Labels,
		"body_length", len(body),
	)

	// Placeholder - in real implementation, use gh CLI or GitHub API
	return fmt.Sprintf("https://github.com/%s/issues/placeholder", d.config.GitHub.Repo), nil
}

// buildIssueBody builds the GitHub issue body from a diagnosis result.
func (d *Diagnostician) buildIssueBody(result *DiagResult) string {
	body := fmt.Sprintf("## Summary\n%s\n\n", result.Preview.Summary)

	if result.Preview.Reproduction != "" {
		body += fmt.Sprintf("### Reproduction Steps\n%s\n\n", result.Preview.Reproduction)
	}

	if result.Preview.Expected != "" {
		body += fmt.Sprintf("### Expected Behavior\n%s\n\n", result.Preview.Expected)
	}

	if result.Preview.Actual != "" {
		body += fmt.Sprintf("### Actual Behavior\n%s\n\n", result.Preview.Actual)
	}

	if result.Preview.RootCause != "" {
		body += fmt.Sprintf("### Root Cause\n%s\n\n", result.Preview.RootCause)
	}

	if result.Preview.SuggestedFix != "" {
		body += fmt.Sprintf("### Suggested Fix\n%s\n\n", result.Preview.SuggestedFix)
	}

	// Add context
	body += "---\n\n"
	body += "### Diagnostic Context\n\n"

	if result.Context.Error != nil {
		body += FormatErrorForIssue(result.Context.Error) + "\n\n"
	}

	if result.Context.Environment != nil {
		body += FormatEnvForIssue(result.Context.Environment) + "\n\n"
	}

	if result.Context.Conversation != nil {
		body += FormatConversationForIssue(result.Context.Conversation) + "\n"
	}

	body += fmt.Sprintf("\n---\n*Auto-generated by HotPlex Diagnostics at %s*\n",
		result.CreatedAt.Format(time.RFC3339))

	return body
}

// CleanupStale removes stale pending diagnoses older than maxAge.
func (d *Diagnostician) CleanupStale(maxAge time.Duration) int {
	var cleaned int
	cutoff := time.Now().Add(-maxAge)

	d.pending.Range(func(key, value any) bool {
		pd := value.(*pendingDiag)
		if pd.createdAt.Before(cutoff) {
			if pd.timer != nil {
				pd.timer.Stop()
			}
			d.pending.Delete(key)
			cleaned++
		}
		return true
	})

	if cleaned > 0 {
		d.logger.Info("Cleaned up stale diagnoses", "count", cleaned)
	}

	return cleaned
}

// MarshalPreview marshals an issue preview to JSON.
func MarshalPreview(preview *IssuePreview) ([]byte, error) {
	return json.MarshalIndent(preview, "", "  ")
}

// UnmarshalPreview unmarshals JSON to an issue preview.
func UnmarshalPreview(data []byte) (*IssuePreview, error) {
	var preview IssuePreview
	err := json.Unmarshal(data, &preview)
	return &preview, err
}
