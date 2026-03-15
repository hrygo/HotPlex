package diag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// IssueCreator handles GitHub issue creation.
type IssueCreator struct {
	config *Config
	logger *slog.Logger
	client *http.Client
}

// NewIssueCreator creates a new IssueCreator.
func NewIssueCreator(config *Config, logger *slog.Logger) *IssueCreator {
	if logger == nil {
		logger = slog.Default()
	}
	return &IssueCreator{
		config: config,
		logger: logger.With("component", "diag.issue"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Create creates a GitHub issue from a diagnostic result.
func (c *IssueCreator) Create(ctx context.Context, result *DiagResult) (*IssueCreationResult, error) {
	if result == nil || result.Preview == nil {
		return nil, fmt.Errorf("invalid result: missing preview")
	}

	// Build issue body
	body := c.buildIssueBody(result)

	// Try gh CLI first (preferred method)
	issueURL, issueNum, err := c.createViaCLI(ctx, result.Preview, body)
	if err == nil {
		return &IssueCreationResult{
			URL:    issueURL,
			Number: issueNum,
		}, nil
	}

	c.logger.Debug("gh CLI not available, trying API", "error", err)

	// Fall back to API
	issueURL, issueNum, err = c.createViaAPI(ctx, result.Preview, body)
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	return &IssueCreationResult{
		URL:    issueURL,
		Number: issueNum,
	}, nil
}

// IssueCreationResult contains the result of issue creation.
type IssueCreationResult struct {
	URL    string
	Number int
}

// createViaCLI creates an issue using the gh CLI.
func (c *IssueCreator) createViaCLI(ctx context.Context, preview *IssuePreview, body string) (string, int, error) {
	// Check if gh is available
	if _, err := exec.LookPath("gh"); err != nil {
		return "", 0, fmt.Errorf("gh CLI not found: %w", err)
	}

	// Build arguments
	args := []string{
		"issue", "create",
		"--repo", c.config.GitHub.Repo,
		"--title", preview.Title,
		"--body", body,
	}

	// Add labels
	for _, label := range preview.Labels {
		args = append(args, "--label", label)
	}

	// Execute
	cmd := exec.CommandContext(ctx, "gh", args...)
	output, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", 0, fmt.Errorf("gh issue create failed: %s", string(ee.Stderr))
		}
		return "", 0, fmt.Errorf("gh issue create: %w", err)
	}

	// Parse output URL
	issueURL := strings.TrimSpace(string(output))

	// Extract issue number from URL
	issueNum := c.extractIssueNumber(issueURL)

	c.logger.Info("Created issue via gh CLI", "url", issueURL, "number", issueNum)

	return issueURL, issueNum, nil
}

// createViaAPI creates an issue using the GitHub API.
func (c *IssueCreator) createViaAPI(ctx context.Context, preview *IssuePreview, body string) (string, int, error) {
	token := c.config.GitHub.Token
	if token == "" {
		// Try to get token from environment
		token = os.Getenv("GITHUB_TOKEN")
	}

	if token == "" {
		return "", 0, fmt.Errorf("no GitHub token available")
	}

	// Build request
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/issues", c.config.GitHub.Repo)

	reqBody := map[string]any{
		"title":  preview.Title,
		"body":   body,
		"labels": preview.Labels,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("api request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", 0, fmt.Errorf("api error: %s - %s", resp.Status, string(respBody))
	}

	// Parse response
	var issueResp struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}

	if err := json.Unmarshal(respBody, &issueResp); err != nil {
		return "", 0, fmt.Errorf("parse response: %w", err)
	}

	c.logger.Info("Created issue via API", "url", issueResp.HTMLURL, "number", issueResp.Number)

	return issueResp.HTMLURL, issueResp.Number, nil
}

// buildIssueBody builds the GitHub issue body.
func (c *IssueCreator) buildIssueBody(result *DiagResult) string {
	var buf strings.Builder

	// Summary
	buf.WriteString("## Summary\n")
	buf.WriteString(result.Preview.Summary)
	buf.WriteString("\n\n")

	// Reproduction
	if result.Preview.Reproduction != "" {
		buf.WriteString("### Reproduction Steps\n")
		buf.WriteString(result.Preview.Reproduction)
		buf.WriteString("\n\n")
	}

	// Expected vs Actual
	if result.Preview.Expected != "" {
		buf.WriteString("### Expected Behavior\n")
		buf.WriteString(result.Preview.Expected)
		buf.WriteString("\n\n")
	}

	if result.Preview.Actual != "" {
		buf.WriteString("### Actual Behavior\n")
		buf.WriteString(result.Preview.Actual)
		buf.WriteString("\n\n")
	}

	// Root cause
	if result.Preview.RootCause != "" {
		buf.WriteString("### Root Cause\n")
		buf.WriteString(result.Preview.RootCause)
		buf.WriteString("\n\n")
	}

	// Suggested fix
	if result.Preview.SuggestedFix != "" {
		buf.WriteString("### Suggested Fix\n")
		buf.WriteString(result.Preview.SuggestedFix)
		buf.WriteString("\n\n")
	}

	// Separator
	buf.WriteString("---\n\n")
	buf.WriteString("### Diagnostic Details\n\n")

	// Error info
	if result.Context.Error != nil {
		buf.WriteString(FormatErrorForIssue(result.Context.Error))
		buf.WriteString("\n\n")
	}

	// Environment
	if result.Context.Environment != nil {
		buf.WriteString(FormatEnvForIssue(result.Context.Environment))
		buf.WriteString("\n\n")
	}

	// Conversation
	if result.Context.Conversation != nil {
		buf.WriteString("<details>\n<summary>Conversation History</summary>\n\n")
		buf.WriteString(FormatConversationForIssue(result.Context.Conversation))
		buf.WriteString("\n</details>\n\n")
	}

	// Logs (if present)
	if len(result.Context.Logs) > 0 {
		buf.WriteString("<details>\n<summary>Recent Logs</summary>\n\n```\n")
		buf.WriteString(string(result.Context.Logs))
		buf.WriteString("\n```\n</details>\n\n")
	}

	// Footer
	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "*Auto-generated by HotPlex Diagnostics at %s*\n",
		result.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&buf, "Diagnosis ID: `%s`\n", result.ID)
	fmt.Fprintf(&buf, "Session: `%s`\n", result.Context.OriginalSessionID)

	return buf.String()
}

// extractIssueNumber extracts the issue number from a GitHub URL.
func (c *IssueCreator) extractIssueNumber(issueURL string) int {
	parsed, err := url.Parse(issueURL)
	if err != nil {
		return 0
	}

	parts := strings.Split(strings.TrimSuffix(parsed.Path, "/"), "/")
	if len(parts) >= 2 {
		var num int
		if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &num); err == nil {
			return num
		}
	}
	return 0
}

// GetIssue retrieves an issue by number.
func (c *IssueCreator) GetIssue(ctx context.Context, number int) (*IssueInfo, error) {
	// Try gh CLI first
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", fmt.Sprintf("%d", number),
		"--repo", c.config.GitHub.Repo,
		"--json", "number,title,state,url,labels")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue view: %w", err)
	}

	var info struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}

	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("parse issue: %w", err)
	}

	labels := make([]string, len(info.Labels))
	for i, l := range info.Labels {
		labels[i] = l.Name
	}

	return &IssueInfo{
		Number: info.Number,
		Title:  info.Title,
		State:  info.State,
		URL:    info.URL,
		Labels: labels,
	}, nil
}

// IssueInfo contains information about a GitHub issue.
type IssueInfo struct {
	Number int
	Title  string
	State  string
	URL    string
	Labels []string
}
