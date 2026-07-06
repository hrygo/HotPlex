package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/pkg/events"
)

// sensitiveToolNames are tools whose full input is recorded in the audit trail
// because their inputs are forensically valuable (spec §5.3: "sensitive
// behaviors store full context directly in detail_json"). Bash/Write/Edit
// command surfaces, MultiEdit bulk edits, and network-fetch tools fall here.
// Add with care — every entry means audit rows grow with full input.
var sensitiveToolNames = map[string]bool{
	"Bash":        true,
	"write":       true, // codex/opencode naming
	"edit":        true,
	"Write":       true,
	"Edit":        true,
	"MultiEdit":   true,
	"str_replace": true, // opencode
	"WebFetch":    true,
	"WebSearch":   true,
	"web_fetch":   true,
	"web_search":  true,
}

// toolInputPreviewLimit caps the non-sensitive tool input preview length
// (UTF-8 runes). Sensitive tools store full input (after sanitization).
const toolInputPreviewLimit = 200

// sensitiveInputPatterns match secret-like substrings inside tool inputs so
// they are masked before the input is stored in the audit trail (spec §5.9:
// "forbidden to log credentials, API key plaintext"). Matches are replaced
// with a prefix+mask token. Patterns are intentionally conservative — false
// negatives (missed secrets) are acceptable, false positives (masked non-
// secrets) are not harmful.
//
// Each pattern MUST define two capture groups:
//
//	group 1 = the literal prefix to preserve verbatim (e.g. "hpk_", "Bearer ",
//	          "password=")
//	group 2 = the secret payload to mask
//
// maskSensitiveInput relies on this two-group contract.
var sensitiveInputPatterns = []*regexp.Regexp{
	// Prefixed API keys: prefix (group 1) + secret payload (group 2).
	regexp.MustCompile(`(?i)((?:hpk_|sk-|sk_|AKIA|ghp_|gho_|ghs_|xox[baprs]-))([A-Za-z0-9]{4,})`),
	// Bearer tokens: literal "Bearer " (group 1) + opaque token (group 2).
	regexp.MustCompile(`(?i)(Bearer\s+)([A-Za-z0-9._\-+]{4,})`),
	// password=/passwd=/pwd=/secret=/token=/api_key= assignments: key (group 1)
	// + secret value (group 2).
	regexp.MustCompile(`(?i)(?:(password|passwd|pwd|secret|token|api[_-]?key)["']?\s*[:=]\s*"?)([^\s"']{4,})`),
}

// maskSensitiveInput redacts secret-like substrings from a rendered tool-input
// string. Returns the sanitized string. Non-matching input is returned as-is.
// Each match is replaced with a prefix(4)+… token so the audit row shows enough
// to identify which key was used without exposing it (spec §5.9 prefix+mask).
func maskSensitiveInput(s string) string {
	for _, re := range sensitiveInputPatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			loc := re.FindStringSubmatchIndex(match)
			// Need at least 3 pairs (full match + 2 capture groups) for the
			// prefix/secret split; otherwise fall back to whole-match masking.
			if len(loc) < 6 || loc[2] < 0 || loc[4] < 0 {
				if utf8.RuneCountInString(match) <= 4 {
					return strings.Repeat("*", len(match))
				}
				return string([]rune(match)[:4]) + "…"
			}
			prefix := match[loc[2]:loc[3]]
			secret := match[loc[4]:loc[5]]
			if utf8.RuneCountInString(secret) <= 4 {
				return prefix + strings.Repeat("*", utf8.RuneCountInString(secret))
			}
			runes := []rune(secret)
			return prefix + string(runes[:4]) + "…"
		})
	}
	return s
}

// renderToolInput converts a tool's Input map to a stable JSON string for the
// audit detail. Returns "" when the input is empty/unrenderable.
func renderToolInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}

// sha256Hex returns the hex-encoded sha256 of s. Used for non-sensitive tool
// input fingerprinting (spec §5.3: non-sensitive stores summary + sha256).
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// truncateRunes returns the first n UTF-8 runes of s, suffixed with "…" when
// truncation occurs.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// emitToolCallAudit enqueues a tool.call audit event. Non-blocking and safe to
// call from the forward path; no-op when auditCollector is nil. Outcome is
// success (see note on ToolCall branch in bridge_forward.go re: failure scope).
// Attribution: UserID comes from fc.sessOwner (resolved earlier via
// sm.Get → OwnerID||UserID, same identity space as message.inbound's
// env.OwnerID). UserIDType is "platform" per spec §5.4 tool.call backtracking
// (session_id → sessions.user_id).
func (b *Bridge) emitToolCallAudit(fc *forwardContext, tc *events.ToolCallData) {
	c := b.auditCollector
	if c == nil {
		return
	}
	userID := fc.sessOwner
	if userID == "" {
		userID = audit.AnonymousUserID
	}
	detail := buildToolCallDetail(tc)
	ua := &audit.UserActivity{
		Ts:           time.Now().UnixMilli(),
		UserID:       userID,
		UserIDType:   audit.UserIDTypePlatform,
		Platform:     fc.sessPlatform,
		SessionID:    fc.sessionID,
		Action:       audit.ActionToolCall,
		ResourceType: "tool",
		ResourceID:   tc.ID,
		Outcome:      audit.OutcomeSuccess,
		DetailJSON:   detail,
	}
	_ = c.Enqueue(context.Background(), ua)
}

// buildToolCallDetail constructs the whitelisted detail_json for a tool.call
// audit row. Per spec §5.3:
//   - Sensitive tools → full input (after secret masking), no sha256 needed.
//   - Non-sensitive tools → input sha256 + truncated preview.
//
// Always records: name, success (true at emit time), kind (if present), title.
func buildToolCallDetail(tc *events.ToolCallData) string {
	rawInput := renderToolInput(tc.Input)
	sensitive := sensitiveToolNames[tc.Name]
	d := map[string]any{
		"name":    tc.Name,
		"success": true, // tool was invoked; failure correlation is P3
	}
	if tc.Kind != "" {
		d["kind"] = tc.Kind
	}
	if tc.Title != "" {
		d["title"] = tc.Title
	}
	switch {
	case sensitive:
		// Full input, secrets masked (spec §5.3 sensitive-behavior full store).
		masked := maskSensitiveInput(rawInput)
		d["input"] = masked
		d["sensitive"] = true
	case rawInput != "":
		// Summary + sha256 (spec §5.3 default).
		d["input_sha256"] = sha256Hex(rawInput)
		d["input_preview"] = truncateRunes(rawInput, toolInputPreviewLimit)
	default:
		// No input payload (e.g. parameterless tool).
		d["input_sha256"] = ""
	}
	b, _ := json.Marshal(d)
	return string(b)
}
