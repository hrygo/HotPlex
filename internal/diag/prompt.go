package diag

// Prompts for LLM-based diagnosis
const (
	// DiagnosePrompt is the main prompt for analyzing errors and generating issue previews.
	DiagnosePrompt = `You are a diagnostic assistant for HotPlex, an AI agent control plane.
Analyze the following error context and generate a structured diagnosis.

## Error Context
%s

## Task
1. Identify the root cause of the error
2. Generate a GitHub issue preview with:
   - A concise title (max 80 chars)
   - Appropriate labels (bug, enhancement, or question)
   - Priority assessment (high, medium, low)
   - Clear reproduction steps
   - Expected vs actual behavior
   - Suggested fix (if identifiable)

## Response Format
Respond with a JSON object matching this structure:
{
  "title": "Brief issue title",
  "labels": ["bug"],
  "priority": "medium",
  "summary": "One-line problem summary",
  "reproduction": "1. Step one\n2. Step two",
  "expected": "What should happen",
  "actual": "What actually happened",
  "root_cause": "Identified root cause (if any)",
  "suggested_fix": "Proposed solution (if any)"
}

Analyze the context and respond with only the JSON object.`

	// SummarizeConversationPrompt summarizes conversation history.
	SummarizeConversationPrompt = `Summarize the following conversation history for diagnostic purposes.
Focus on:
1. Key user requests
2. Assistant responses
3. Any error messages or anomalies
4. The sequence of events leading to the current state

Keep the summary under %d bytes.

## Conversation:
%s

## Summary:
Provide a concise summary that captures the essential context for debugging.`

	// AnalyzeErrorPrompt analyzes error patterns.
	AnalyzeErrorPrompt = `Analyze the following error and classify its severity and category.

## Error:
Type: %s
Message: %s
Exit Code: %d

## Context:
%s

## Classification:
1. Severity: critical/high/medium/low
2. Category: network/auth/resource/logic/configuration/unknown
3. Is recoverable: true/false
4. Recommended action: brief description

Respond with only a JSON object:
{
  "severity": "high",
  "category": "network",
  "recoverable": false,
  "recommended_action": "Check network connectivity"
}`
)

// BuildDiagnosisContext builds the context string for the diagnosis prompt.
func BuildDiagnosisContext(diagCtx *DiagContext) string {
	context := "### Session Information\n"
	context += "- Session ID: " + diagCtx.OriginalSessionID + "\n"
	context += "- Platform: " + diagCtx.Platform + "\n"
	context += "- Trigger: " + string(diagCtx.Trigger) + "\n"
	context += "- Time: " + diagCtx.Timestamp.Format("2006-01-02 15:04:05") + "\n\n"

	if diagCtx.Error != nil {
		context += "### Error\n"
		context += "- Type: " + string(diagCtx.Error.Type) + "\n"
		context += "- Message: " + diagCtx.Error.Message + "\n"
		if diagCtx.Error.ExitCode != 0 {
			context += "- Exit Code: " + string(rune(diagCtx.Error.ExitCode)) + "\n"
		}
		context += "\n"
	}

	if diagCtx.Conversation != nil {
		context += "### Recent Conversation\n"
		context += "- Messages: " + string(rune(diagCtx.Conversation.MessageCount)) + "\n"
		if diagCtx.Conversation.IsSummarized {
			context += "- (Summarized)\n"
		}
		context += "```\n" + diagCtx.Conversation.Processed + "\n```\n\n"
	}

	if diagCtx.Environment != nil {
		context += "### Environment\n"
		context += "- Version: " + diagCtx.Environment.HotPlexVersion + "\n"
		context += "- OS: " + diagCtx.Environment.OS + "/" + diagCtx.Environment.Arch + "\n"
		context += "- Uptime: " + diagCtx.Environment.Uptime.String() + "\n"
	}

	if len(diagCtx.Logs) > 0 {
		context += "### Recent Logs\n```\n" + string(diagCtx.Logs) + "\n```\n"
	}

	return context
}
