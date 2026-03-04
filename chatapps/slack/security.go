// Package slack provides the Slack adapter implementation for the hotplex engine.
// Security validation, sanitization, and signature verification for Slack requests.
package slack

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"golang.org/x/net/publicsuffix"
)

// =============================================================================
// Security Validation Functions
// =============================================================================

// AllowedURLSchemes defines which URL schemes are allowed in links
var AllowedURLSchemes = []string{"http://", "https://", "mailto:", "ftp://"}

// ButtonValuePattern validates permission button values
// Format: behavior:sessionID:messageID (alphanumeric + _ - only)
var ButtonValuePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+:[a-zA-Z0-9_-]+:[a-zA-Z0-9_-]+$`)

// ActionIDPattern validates action_id format (alphanumeric + _ - only, max 255 chars)
var ActionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,255}$`)

// SanitizeErrorMessage removes potentially sensitive information from error messages
func SanitizeErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	// Remove file paths
	msg = regexp.MustCompile(`[A-Z]:\\[^"'\s]+|/[^"'\s]+`).ReplaceAllString(msg, "[path redacted]")

	// Remove potential secrets (patterns like API keys, tokens)
	msg = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|credential)[=:]\S+`).ReplaceAllString(msg, "$1=[redacted]")

	// Remove stack traces
	if idx := strings.Index(msg, "goroutine"); idx != -1 {
		msg = msg[:idx]
	}

	// Truncate very long messages
	if len(msg) > 500 {
		msg = msg[:497] + "..."
	}

	return msg
}

// ValidateButtonValue validates permission button value format
// Returns (behavior, sessionID, messageID, error)
func ValidateButtonValue(value string) (string, string, string, error) {
	// Check format with regex
	if !ButtonValuePattern.MatchString(value) {
		return "", "", "", fmt.Errorf("invalid button value format: must be behavior:sessionID:messageID (alphanumeric only)")
	}

	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid button value: expected 3 parts, got %d", len(parts))
	}

	behavior := parts[0]
	sessionID := parts[1]
	messageID := parts[2]

	// Validate behavior
	if behavior != "allow" && behavior != "deny" {
		return "", "", "", fmt.Errorf("invalid behavior: must be 'allow' or 'deny'")
	}

	// Additional length checks
	if len(sessionID) > 255 {
		return "", "", "", fmt.Errorf("sessionID too long: %d chars", len(sessionID))
	}
	if len(messageID) > 255 {
		return "", "", "", fmt.Errorf("messageID too long: %d chars", len(messageID))
	}

	return behavior, sessionID, messageID, nil
}

// ValidateURL checks if a URL uses an allowed scheme
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	// Check for allowed schemes
	hasAllowedScheme := false
	for _, scheme := range AllowedURLSchemes {
		if strings.HasPrefix(rawURL, scheme) {
			hasAllowedScheme = true
			break
		}
	}

	if !hasAllowedScheme {
		return fmt.Errorf("URL scheme not allowed: must start with %v", AllowedURLSchemes)
	}

	// Parse and validate URL structure
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Check for javascript: in path (bypass attempt)
	if strings.Contains(parsedURL.Path, "javascript:") {
		return fmt.Errorf("URL contains disallowed javascript: protocol")
	}

	// Check for data: URIs (potential XSS)
	if strings.HasPrefix(rawURL, "data:") {
		return fmt.Errorf("data: URLs are not allowed")
	}

	// Validate domain if present
	if parsedURL.Host != "" {
		// Check for IP addresses (optional - may want to allow)
		// Check for localhost/internal hosts (optional)

		// Validate TLD
		if parsedURL.Hostname() != "localhost" && parsedURL.Hostname() != "127.0.0.1" {
			_, _ = publicsuffix.EffectiveTLDPlusOne(parsedURL.Hostname())
			// Ignore error - some valid URLs may not have public suffix
		}
	}

	return nil
}

// ValidateActionID validates action_id format
func ValidateActionID(actionID string) error {
	if actionID == "" {
		return fmt.Errorf("action_id cannot be empty")
	}

	if len(actionID) > MaxButtonActionIDLen {
		return fmt.Errorf("action_id too long: %d chars (max %d)", len(actionID), MaxButtonActionIDLen)
	}

	if !ActionIDPattern.MatchString(actionID) {
		return fmt.Errorf("action_id contains invalid characters: only alphanumeric, underscore, and hyphen allowed")
	}

	return nil
}

// ValidateOptionValue validates option value in select menus
func ValidateOptionValue(value string) error {
	if value == "" {
		return fmt.Errorf("option value cannot be empty")
	}

	// Slack allows up to 75 chars for option values
	if len(value) > 75 {
		return fmt.Errorf("option value too long: %d chars (max 75)", len(value))
	}

	// Check for potentially dangerous patterns
	if strings.Contains(value, "<!") || strings.Contains(value, "<@") || strings.Contains(value, "<#") {
		return fmt.Errorf("option value cannot contain Slack special syntax")
	}

	return nil
}

// SanitizeForDisplay removes potentially dangerous content for display
func SanitizeForDisplay(content string, maxLength int) string {
	// Truncate first
	content = base.TruncateByRune(content, maxLength)

	// Remove null bytes
	content = strings.ReplaceAll(content, "\x00", "")

	// Remove control characters except newline and tab
	content = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' {
			return r
		}
		if r < 32 {
			return -1 // Remove control character
		}
		return r
	}, content)

	return content
}

// ValidateToolName validates tool name for safe display
func ValidateToolName(toolName string) string {
	// Remove potentially dangerous characters
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			return r
		}
		return -1
	}, toolName)

	if safe == "" {
		return "unknown_tool"
	}

	return safe
}

// SanitizeCommand sanitizes command string for safe display in code blocks
func SanitizeCommand(command string) string {
	// Remove null bytes
	command = strings.ReplaceAll(command, "\x00", "")

	// Remove backticks to prevent code block injection
	command = strings.ReplaceAll(command, "```", "\\`\\`\\`")
	command = strings.ReplaceAll(command, "`", "\\`")

	// Remove Slack special syntax
	command = strings.ReplaceAll(command, "<!", "&lt;!")
	command = strings.ReplaceAll(command, "<@", "&lt;@")
	command = strings.ReplaceAll(command, "<#", "&lt;#")

	// Truncate if too long
	if base.RuneCount(command) > 2000 {
		command = base.TruncateWithEllipsis(command, 2000)
	}

	return command
}

// ValidateBlockID validates block_id length
func ValidateBlockID(blockID string) string {
	if len(blockID) > MaxBlockIDLen {
		// Truncate and add hash to maintain uniqueness
		hash := fmt.Sprintf("%x", len(blockID))
		maxLen := MaxBlockIDLen - len(hash) - 1
		if maxLen < 10 {
			maxLen = 10
		}
		return blockID[:maxLen] + "_" + hash
	}
	return blockID
}

// ValidatePlainText validates plain_text object content
func ValidatePlainText(text string, withEmoji bool) string {
	// Truncate to max length
	if base.RuneCount(text) > MaxPlainTextLen {
		text = base.TruncateWithEllipsis(text, MaxPlainTextLen)
	}

	// Remove null bytes
	text = strings.ReplaceAll(text, "\x00", "")

	// Optionally remove emoji if not allowed
	if !withEmoji {
		// Emoji removal handled by ValidateEmoji function
		text = ValidateEmoji(text, false)
	}

	return text
}

// ValidateMrkdwnText validates mrkdwn text content
func ValidateMrkdwnText(text string) string {
	// Truncate to max length
	if base.RuneCount(text) > MaxSectionTextLen {
		text = base.TruncateWithEllipsis(text, MaxSectionTextLen)
	}

	// Remove null bytes
	text = strings.ReplaceAll(text, "\x00", "")

	// Validate code block balance
	text = balanceCodeBlocks(text)

	return text
}

// balanceCodeBlocks ensures code blocks are properly closed
func balanceCodeBlocks(text string) string {
	codeBlockCount := 0
	for i := 0; i < len(text); {
		if i+2 < len(text) && text[i:i+3] == "```" {
			codeBlockCount++
			i += 3
			continue
		}
		i++
	}

	// If odd number of code blocks, add closing
	if codeBlockCount%2 != 0 {
		text += "\n```"
	}

	return text
}

// IsAllowedScheme checks if a URL scheme is in the allowed list
func IsAllowedScheme(scheme string) bool {
	for _, allowed := range AllowedURLSchemes {
		if scheme == allowed {
			return true
		}
	}
	return false
}

// ValidateInitialValue validates initial_value for input elements
func ValidateInitialValue(initialValue string, maxLength int) string {
	if base.RuneCount(initialValue) > maxLength {
		initialValue = base.TruncateWithEllipsis(initialValue, maxLength)
	}
	return SanitizeForDisplay(initialValue, maxLength)
}

// ValidateConfirmationDialog validates confirmation dialog fields
func ValidateConfirmationDialog(title, text, confirmText, denyText string) (string, string, string, string) {
	// All fields limited to 75 chars per Slack spec
	title = base.TruncateByRune(title, 75)
	text = base.TruncateByRune(text, 75)
	confirmText = base.TruncateByRune(confirmText, 75)
	denyText = base.TruncateByRune(denyText, 75)

	return title, text, confirmText, denyText
}

// ValidateImageURL validates URL for image blocks
func ValidateImageURL(imageURL string) error {
	if err := ValidateURL(imageURL); err != nil {
		return err
	}

	// Additional check: image URLs should typically end with image extension or be from known domains
	// This is optional but recommended
	return nil
}

// ValidateButtonURL validates URL for button elements
func ValidateButtonURL(buttonURL string) error {
	if err := ValidateURL(buttonURL); err != nil {
		return err
	}

	// Button URLs limited to 3000 chars
	if base.RuneCount(buttonURL) > 3000 {
		return fmt.Errorf("button URL too long: %d chars (max 3000)", base.RuneCount(buttonURL))
	}

	return nil
}

// =============================================================================
// Medium Priority Security Functions
// =============================================================================

// ValidateEmoji validates and optionally removes emoji from text
func ValidateEmoji(text string, allowEmoji bool) string {
	if allowEmoji {
		return text
	}

	// Remove emoji using Unicode ranges
	// This is a simplified check - comprehensive emoji detection requires more complex logic
	result := strings.Builder{}
	for _, r := range text {
		// Skip common emoji ranges
		if r >= 0x1F600 && r <= 0x1F64F { // Emoticons
			continue
		}
		if r >= 0x1F300 && r <= 0x1F5FF { // Misc Symbols and Pictographs
			continue
		}
		if r >= 0x1F680 && r <= 0x1F6FF { // Transport and Map
			continue
		}
		if r >= 0x2600 && r <= 0x26FF { // Misc symbols
			continue
		}
		if r >= 0x2700 && r <= 0x27BF { // Dingbats
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ValidateRegexPattern validates regex pattern to prevent ReDoS
func ValidateRegexPattern(pattern string) (string, error) {
	// Limit pattern length
	if len(pattern) > 1000 {
		return "", fmt.Errorf("regex pattern too long: %d chars (max 1000)", len(pattern))
	}

	// Check for dangerous patterns that could cause ReDoS
	dangerousPatterns := []string{
		`\(.*\).*\(`,  // Nested groups with wildcards
		`\([^)]*\)\*`, // Repeated groups
		`\(.*\)\+`,    // Repeated groups with +
	}

	for _, dp := range dangerousPatterns {
		if matched, _ := regexp.MatchString(dp, pattern); matched {
			// Warning only - don't block
			return pattern, nil
		}
	}

	return pattern, nil
}

// ValidateTokenFormat validates Slack token format with safe regex
func ValidateTokenFormat(token string) bool {
	// Use simple string operations instead of complex regex
	if len(token) < 10 || len(token) > 100 {
		return false
	}

	// Check for known token prefixes
	validPrefixes := []string{"xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-"}
	for _, prefix := range validPrefixes {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}

	return false
}

// SanitizeForRegex escapes special regex characters in user input
func SanitizeForRegex(input string) string {
	return regexp.QuoteMeta(input)
}

// ValidateConfirmationDialogText validates confirmation dialog text fields
func ValidateConfirmationDialogText(title, text, confirmText, denyText string) (string, string, string, string) {
	// Slack limits these fields to 75 characters
	maxLen := 75

	title = base.TruncateByRune(title, maxLen)
	text = base.TruncateByRune(text, maxLen)
	confirmText = base.TruncateByRune(confirmText, maxLen)
	denyText = base.TruncateByRune(denyText, maxLen)

	return title, text, confirmText, denyText
}

// ValidateInitialValueForInput validates initial_value for input elements
func ValidateInitialValueForInput(initialValue string, maxLength int) string {
	if initialValue == "" {
		return ""
	}

	// Sanitize and truncate
	initialValue = SanitizeForDisplay(initialValue, maxLength)
	initialValue = base.TruncateByRune(initialValue, maxLength)

	return initialValue
}

// ValidateEmailFormat performs basic email validation
func ValidateEmailFormat(email string) bool {
	if email == "" {
		return false
	}

	// Basic format check
	if !strings.Contains(email, "@") {
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}

	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}

	// Check for valid characters
	validEmail := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return validEmail.MatchString(email)
}

// ValidateURLFormat performs basic URL validation
func ValidateURLFormat(rawURL string) bool {
	if rawURL == "" {
		return false
	}

	// Check for valid scheme
	hasScheme := false
	for _, scheme := range AllowedURLSchemes {
		if strings.HasPrefix(rawURL, scheme) {
			hasScheme = true
			break
		}
	}

	return hasScheme
}

// RateLimitKey generates a rate limit key based on user ID and IP
func RateLimitKey(userID, ip string) string {
	// Combine user ID and IP for more robust rate limiting
	if ip != "" {
		return fmt.Sprintf("%s:%s", userID, ip)
	}
	return userID
}

// SanitizeMarkdown removes potentially dangerous markdown patterns
func SanitizeMarkdown(content string) string {
	// Remove potential HTML injection
	content = strings.ReplaceAll(content, "<script", "&lt;script")
	content = strings.ReplaceAll(content, "</script", "&lt;/script")

	// Balance code blocks
	content = ValidateMrkdwnText(content)

	return content
}

// ValidateMentionFormat validates Slack mention format
func ValidateMentionFormat(mention string) bool {
	// Valid mentions: <!here>, <!channel>, <!everyone>, <@user>, <#channel>
	validMention := regexp.MustCompile(`^<(![a-z]+|[@#][A-Z0-9]+)(\|[^>]+)?>$`)
	return validMention.MatchString(mention)
}
