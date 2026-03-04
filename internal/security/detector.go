package security

import (
	"bufio"
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/strutil"
)

// Constants for danger detector logging and display limits.
const (
	// Maximum input length to log (prevents log flooding)
	MaxInputLogLength = 50
	// Maximum pattern match length to log
	MaxPatternLogLength = 100
	// Maximum command display length for UI
	MaxDisplayLength = 100
)

// ========================================
// RuleSource Interfaces (for extensibility)
// ========================================

// RuleSource defines an interface for loading security rules.
type RuleSource interface {
	LoadRules(ctx context.Context) ([]SecurityRule, error)
	Name() string
}

// RuleUpdate represents a rule change event.
type RuleUpdate struct {
	Type      RuleUpdateType
	Rule      SecurityRule
	Timestamp time.Time
}

// RuleUpdateType represents the type of rule update.
type RuleUpdateType string

const (
	RuleAdd      RuleUpdateType = "add"
	RuleRemove   RuleUpdateType = "remove"
	RuleUpdateOp RuleUpdateType = "update"
)

// ========================================
// AuditStore Interfaces (for observability)
// ========================================

// AuditAction represents the action taken on an input.
type AuditAction string

const (
	AuditActionBlocked  AuditAction = "blocked"
	AuditActionApproved AuditAction = "approved"
	AuditActionBypassed AuditAction = "bypassed"
)

// AuditEvent represents a security audit log entry.
type AuditEvent struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Input     string         `json:"input"` // Truncated input
	Operation string         `json:"operation"`
	Reason    string         `json:"reason"`
	Level     DangerLevel    `json:"level"`
	Category  string         `json:"category"`
	Action    AuditAction    `json:"action"`
	UserID    string         `json:"user_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Source    string         `json:"source"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// AuditFilter for querying audit events.
type AuditFilter struct {
	StartTime  time.Time
	EndTime    time.Time
	Levels     []DangerLevel
	Categories []string
	Actions    []AuditAction
	UserID     string
	SessionID  string
	Limit      int
}

// AuditStats contains aggregated statistics.
type AuditStats struct {
	TotalBlocked  int64            `json:"total_blocked"`
	TotalApproved int64            `json:"total_approved"`
	ByLevel       map[string]int64 `json:"by_level"`
	ByCategory    map[string]int64 `json:"by_category"`
	BySource      map[string]int64 `json:"by_source"`
	TopPatterns   []PatternStat    `json:"top_patterns"`
	TimeSeries    []TimeBucket     `json:"time_series"`
}

// PatternStat represents a pattern and its hit count.
type PatternStat struct {
	Pattern string `json:"pattern"`
	Count   int64  `json:"count"`
}

// TimeBucket represents a time bucket for time-series data.
type TimeBucket struct {
	Timestamp time.Time `json:"timestamp"`
	Count     int64     `json:"count"`
}

// AuditStore defines an interface for storing audit logs.
type AuditStore interface {
	Save(ctx context.Context, event *AuditEvent) error
	Query(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)
	Stats(ctx context.Context) (AuditStats, error)
	Close() error
}

// SafePatternRule implements SecurityRule for allowlisted safe commands.
type SafePatternRule struct {
	Pattern     *regexp.Regexp
	Description string
	Category    string
}

// Evaluate checks if the input matches the safe pattern.
func (r *SafePatternRule) Evaluate(input string) *DangerBlockEvent {
	if r.Pattern.MatchString(input) {
		return &DangerBlockEvent{
			Operation:      extractCommand(input, r.Pattern),
			Reason:         r.Description,
			PatternMatched: r.Pattern.String(),
			Level:          DangerLevelSafe,
			Category:       r.Category,
			BypassAllowed:  true,
			Suggestions:    nil,
		}
	}
	return nil
}

// DangerLevel classifies the severity of a detected potentially harmful operation.
type DangerLevel int

const (
	// DangerLevelSafe represents safe commands that are allowlisted.
	DangerLevelSafe DangerLevel = -1
	// DangerLevelCritical represents irreparable damage (e.g., recursive root deletion or disk wiping).
	DangerLevelCritical DangerLevel = iota
	// DangerLevelHigh represents significant damage potential (e.g., deleting user home or system config).
	DangerLevelHigh
	// DangerLevelModerate represents unintended side effects (e.g., resetting Git history).
	DangerLevelModerate
)

// SecurityRule defines an interface for evaluating whether input is dangerous.
type SecurityRule interface {
	// Evaluate analyzes the input command. Return non-nil DangerBlockEvent if blocked.
	Evaluate(input string) *DangerBlockEvent
}

// RegexRule implements SecurityRule using regular expressions.
type RegexRule struct {
	Pattern     *regexp.Regexp // The compiled regex identifying the dangerous sequence
	Description string         // Human-readable explanation of why this pattern is blocked
	Level       DangerLevel    // Severity used for logging and alerting
	Category    string         // Functional category
}

// Evaluate checks if the regex matches the input.
func (r *RegexRule) Evaluate(input string) *DangerBlockEvent {
	if r.Pattern.MatchString(input) {
		return &DangerBlockEvent{
			Operation:      extractCommand(input, r.Pattern),
			Reason:         r.Description,
			PatternMatched: r.Pattern.String(),
			Level:          r.Level,
			Category:       r.Category,
			BypassAllowed:  false,
			Suggestions:    getSuggestions(r.Category),
		}
	}
	return nil
}

// DangerBlockEvent contains detailed forensics after a dangerous operation is successfully intercepted.
type DangerBlockEvent struct {
	Operation      string      `json:"operation"`             // The specific command line that triggered the block
	Reason         string      `json:"reason"`                // Description of the threat
	PatternMatched string      `json:"pattern_matched"`       // The specific signature that matched the input
	Level          DangerLevel `json:"level"`                 // Severity level
	Category       string      `json:"category"`              // Category classification
	BypassAllowed  bool        `json:"bypass_allowed"`        // Whether the user has administrative privileges to bypass this block
	Suggestions    []string    `json:"suggestions,omitempty"` // Safe alternatives to the blocked command
}

// Detector acts as a Web Application Firewall (WAF) for the local system.
// It inspects LLM-generated commands before they are dispatched to the host shell,
// enforcing strict security boundaries regardless of the model's own safety alignment.
type Detector struct {
	rules  []SecurityRule // Registry of active security signatures
	logger *slog.Logger   // Event logger for security forensics
	mu     sync.RWMutex   // Protects concurrent configuration updates

	// ruleSource provides extensible rule loading (optional)
	ruleSource RuleSource

	// auditStore provides audit logging (optional)
	auditStore AuditStore

	// allowPaths defines a whitelist of directories where file operations are permitted.
	allowPaths []string

	// bypassEnabled allows the security layer to be deactivated (admin/evolution mode only).
	bypassEnabled bool

	// adminToken is required to toggle bypassEnabled.
	adminToken string
}

func NewDetector(logger *slog.Logger) *Detector {
	if logger == nil {
		logger = slog.Default()
	}

	dd := &Detector{
		logger: logger,
		rules:  make([]SecurityRule, 0),
	}

	// Load default dangerous patterns
	dd.loadDefaultPatterns()

	// Load safe patterns (allowlisted commands)
	dd.loadSafePatterns()

	return dd
}

// RegisterRule allows injecting custom security rules to extend the WAF.
func (dd *Detector) RegisterRule(rule SecurityRule) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.rules = append(dd.rules, rule)
}

// SetAdminToken sets the token required to toggle bypass mode.
func (dd *Detector) SetAdminToken(token string) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.adminToken = token
}

// SetRuleSource sets the rule source for extensible rule loading.
func (dd *Detector) SetRuleSource(source RuleSource) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.ruleSource = source
	if source != nil {
		dd.loadRulesFromSource(source)
	}
}

// SetAuditStore sets the audit store for logging security events.
func (dd *Detector) SetAuditStore(store AuditStore) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	dd.auditStore = store
}

// loadRulesFromSource loads rules from the provided RuleSource.
func (dd *Detector) loadRulesFromSource(source RuleSource) {
	rules, err := source.LoadRules(context.Background())
	if err != nil {
		dd.logger.Error("Failed to load rules from source",
			"source", source.Name(),
			"error", err)
		return
	}
	dd.rules = append(dd.rules, rules...)
	dd.logger.Info("Loaded rules from source",
		"source", source.Name(),
		"count", len(rules))
}

// loadDefaultPatterns initializes the built-in dangerous command patterns.
func (dd *Detector) loadDefaultPatterns() {
	patterns := []struct {
		pattern     string
		description string
		level       DangerLevel
		category    string
	}{
		// ========================================
		// CRITICAL: Command Injection Bypass Patterns (#6)
		// ========================================
		// Command substitution forms
		{`\$\([^)]*\)`, "Command substitution $() - potential injection", DangerLevelCritical, "injection"},
		{"`[^`]*`", "Backtick command substitution - potential injection", DangerLevelCritical, "injection"},
		// Nested command substitution (bypass attempt)
		{`\$\([^)]*\$\([^)]*\)`, "Nested command substitution - injection bypass attempt", DangerLevelCritical, "injection"},
		{"`[^`]*`[^`]*`", "Nested backtick substitution - injection bypass attempt", DangerLevelCritical, "injection"},
		{`eval\s+`, "Eval command execution", DangerLevelCritical, "injection"},
		{`exec\s+`, "Exec command replacement", DangerLevelHigh, "injection"},

		// Encoding/decoding chains (often used to bypass WAF)
		{`base64\s+-d\s*\|.*sh`, "Base64 decode and execute", DangerLevelCritical, "injection"},
		{`base64\s+-d\s*\|.*bash`, "Base64 decode and execute via bash", DangerLevelCritical, "injection"},
		{`xxd\s+-r\s*\|.*sh`, "Hex decode and execute", DangerLevelCritical, "injection"},
		{`printf\s+.*\\x.*\|`, "Hex escape and pipe", DangerLevelCritical, "injection"},
		{`od\s+-A\s+n\s*-t\s+x1`, "Octal/hex dump for obfuscation", DangerLevelHigh, "injection"},

		// ========================================
		// CRITICAL: Privilege Escalation (#7)
		// ========================================
		{`\bsudo\s+`, "Sudo privilege escalation", DangerLevelHigh, "privilege"},
		{`\bsu\s+`, "Switch user command", DangerLevelHigh, "privilege"},
		{`\bdoas\s+`, "Doas privilege escalation", DangerLevelHigh, "privilege"},
		{`\bpkexec\s+`, "Polkit privilege escalation", DangerLevelCritical, "privilege"},
		{`chmod\s+[ug]\+s\s+`, "Set SUID/SGID bit", DangerLevelCritical, "privilege"},
		{`setcap\s+`, "Set file capabilities", DangerLevelHigh, "privilege"},

		// ========================================
		// CRITICAL: Network Penetration (#7)
		// ========================================
		{`\bnc\s+.*-e\s+`, "Netcat reverse shell", DangerLevelCritical, "network"},
		{`\bncat\s+.*-e\s+`, "Ncat reverse shell", DangerLevelCritical, "network"},
		{`bash\s+-i\s*(&gt;|>)`, "Bash reverse shell", DangerLevelCritical, "network"},
		{`python\s+-c\s+.*socket`, "Python socket reverse shell", DangerLevelCritical, "network"},
		{`perl\s+-e\s+.*socket`, "Perl socket reverse shell", DangerLevelCritical, "network"},
		{`\bmsfconsole\b`, "Metasploit console", DangerLevelCritical, "network"},
		{`\bmsfvenom\b`, "Metasploit payload generator", DangerLevelCritical, "network"},

		// ========================================
		// HIGH: Persistence Mechanisms (#7)
		// ========================================
		{`crontab\s+-[er]`, "Edit crontab (persistence)", DangerLevelHigh, "persistence"},
		{`systemctl\s+enable\s+`, "Enable systemd service (persistence)", DangerLevelHigh, "persistence"},
		{`launchctl\s+load\s+`, "Load launchd service (persistence)", DangerLevelHigh, "persistence"},
		{`>>\s*~/\.bashrc`, "Append to bashrc (persistence)", DangerLevelHigh, "persistence"},
		{`>>\s*~/\.profile`, "Append to profile (persistence)", DangerLevelHigh, "persistence"},
		{`>>\s*~/\.zshrc`, "Append to zshrc (persistence)", DangerLevelHigh, "persistence"},
		{`/etc/rc\.local`, "Modify rc.local (persistence)", DangerLevelHigh, "persistence"},
		{`/etc/init\.d/`, "Modify init.d script (persistence)", DangerLevelHigh, "persistence"},

		// ========================================
		// HIGH: Information Gathering (#7)
		// ========================================
		{`cat\s+/etc/passwd`, "Read password file", DangerLevelHigh, "recon"},
		{`cat\s+/etc/shadow`, "Read shadow file", DangerLevelCritical, "recon"},
		{`cat\s+.*\.ssh/id_rsa`, "Read SSH private key", DangerLevelCritical, "recon"},
		{`cat\s+.*\.ssh/authorized_keys`, "Read SSH authorized keys", DangerLevelHigh, "recon"},
		{`\benv\b.*(?i)password`, "Environment variable password exposure", DangerLevelHigh, "recon"},
		{`\bprintenv\b`, "Print all environment variables", DangerLevelModerate, "recon"},
		{`cat\s+/proc/.*/environ`, "Read process environment", DangerLevelHigh, "recon"},
		{`/proc/self/environ`, "Read own process environment", DangerLevelHigh, "recon"},

		// ========================================
		// CRITICAL: Container Escape (#7)
		// ========================================
		{`docker\s+run\s+.*--privileged`, "Privileged Docker container", DangerLevelCritical, "container"},
		{`docker\s+run\s+.*--network\s+host`, "Host network Docker container", DangerLevelHigh, "container"},
		{`docker\s+run\s+.*-v\s+/`, "Docker volume mount from root", DangerLevelHigh, "container"},
		{`kubectl\s+exec\s+.*--`, "Kubectl exec into pod", DangerLevelHigh, "container"},
		{`\bchroot\s+`, "Chroot escape attempt", DangerLevelHigh, "container"},
		{`\bunshare\s+`, "Unshare namespace", DangerLevelHigh, "container"},
		{`nsenter\s+`, "Enter namespace", DangerLevelHigh, "container"},

		// ========================================
		// CRITICAL: Kernel Module Manipulation (#7)
		// ========================================
		{`\binsmod\s+`, "Insert kernel module", DangerLevelCritical, "kernel"},
		{`\bmodprobe\s+`, "Load kernel module", DangerLevelCritical, "kernel"},
		{`\brmmod\s+`, "Remove kernel module", DangerLevelHigh, "kernel"},
		{`/lib/modules/`, "Direct kernel module access", DangerLevelHigh, "kernel"},

		// ========================================
		// Original patterns (File deletion, etc.)
		// ========================================
		// Critical: File deletion
		{`rm\s+-rf\s+?/`, "Delete root directory", DangerLevelCritical, "file_delete"},
		{`rm\s+-rf\s+?\*/\*`, "Delete all files recursively", DangerLevelCritical, "file_delete"},
		{`rm\s+-rf\s+?[~\w/]+/\*`, "Delete directory contents", DangerLevelHigh, "file_delete"},
		{`rm\s+-[rf]+\s+?/`, "Force delete from root", DangerLevelCritical, "file_delete"},
		{`rmdir\s+?/`, "Remove root directory", DangerLevelCritical, "file_delete"},
		{`del\s+?/[^/\s]*`, "Windows delete root", DangerLevelCritical, "file_delete"},

		// Critical: Filesystem operations
		{`mkfs\.\w+`, "Format filesystem", DangerLevelCritical, "system"},
		{`dd\s+if=/dev/zero`, "Wipe disk with zeros", DangerLevelCritical, "system"},
		{`dd\s+if=/dev/random`, "Wipe disk with random data", DangerLevelCritical, "system"},
		{`dd\s+of=/dev/`, "Write directly to device", DangerLevelCritical, "system"},
		{`wipefs\s+`, "Wipe filesystem signature", DangerLevelCritical, "system"},

		// Critical: Kernel/Process manipulation
		{`:\(.*\)\{.*\}\|`, "Fork bomb pattern", DangerLevelCritical, "system"},
		{`kill\s+-9\s+-1`, "Kill all processes", DangerLevelCritical, "system"},
		{`pkill\s+-9`, "Kill processes by name", DangerLevelHigh, "system"},
		{`killall\s+-9`, "Kill all processes by name", DangerLevelHigh, "system"},

		// High: Destructive overwrites
		{`>\s+/`, "Overwrite root file", DangerLevelCritical, "file_delete"},
		{`>\s+/dev/`, "Write to device file", DangerLevelHigh, "system"},
		{`echo\s+.*>\s+/`, "Echo to root path", DangerLevelHigh, "file_delete"},
		{`truncate\s+-s\s+0\s+/`, "Truncate root file", DangerLevelHigh, "file_delete"},

		// High: System configuration
		{`chmod\s+000\s+/`, "Remove all permissions from root", DangerLevelCritical, "permission"},
		{`chmod\s+-R\s+000\s+`, "Recursively remove all permissions", DangerLevelHigh, "permission"},
		{`chown\s+-R\s+root:root\s+/`, "Change ownership of root recursively", DangerLevelHigh, "permission"},
		{`userdel\s+-r\s+`, "Delete user with home directory", DangerLevelHigh, "system"},

		// High: Network/Dangerous downloads
		{`curl\s+.*\|.*sh`, "Download and execute script via pipe", DangerLevelHigh, "network"},
		{`wget\s+.*\|.*sh`, "Download and execute script via pipe", DangerLevelHigh, "network"},
		{`curl\s+.*\|.*bash`, "Download and execute via bash", DangerLevelHigh, "network"},
		{`wget\s+.*\|.*bash`, "Download and execute via bash", DangerLevelHigh, "network"},
		{`sh\s+-c\s+.*\$\(.*curl`, "Execute downloaded content", DangerLevelHigh, "network"},
		{`sh\s+-c\s+.*\$\(.*wget`, "Execute downloaded content", DangerLevelHigh, "network"},

		// High: Package manipulation
		{`apt-get\s+remove\s+--purge\s+.*essential`, "Remove essential packages", DangerLevelHigh, "system"},
		{`apt\s+remove\s+.*essential`, "Remove essential packages", DangerLevelHigh, "system"},
		{`yum\s+remove\s+.*kernel`, "Remove kernel packages", DangerLevelHigh, "system"},
		{`dpkg\s+--remove\s+--force`, "Force remove packages", DangerLevelHigh, "system"},

		// High: Database operations
		{`DROP\s+DATABASE`, "SQL: Drop database", DangerLevelHigh, "database"},
		{`DELETE\s+FROM.*\bWHERE\b`, "SQL: Delete without proper WHERE", DangerLevelModerate, "database"},
		{`TRUNCATE\s+(TABLE|SCHEMA)`, "SQL: Truncate table/schema", DangerLevelHigh, "database"},
		{`rm\s+.*\.db`, "Delete database file", DangerLevelHigh, "database"},
		{`rm\s+.*\.sqlite`, "Delete SQLite database", DangerLevelHigh, "database"},

		// Moderate: Git operations (data loss potential)
		{`git\s+reset\s+--hard\s+HEAD`, "Reset to HEAD (loses uncommitted changes)", DangerLevelModerate, "git"},
		{`git\s+clean\s+-fd`, "Remove untracked files", DangerLevelModerate, "git"},
		{`git\s+branch\s+-D`, "Force delete branch", DangerLevelModerate, "git"},
		{`rm\s+-rf\s+.*\.git`, "Delete git repository", DangerLevelHigh, "git"},

		// Moderate: SSH/Remote execution
		{`ssh\s+.*\|.*rm`, "Remote delete command", DangerLevelHigh, "network"},
		{`scp\s+.*\s+/\s*$`, "Copy to root directory", DangerLevelHigh, "network"},
	}

	for _, p := range patterns {
		// Compile pattern with case-insensitive flag for flexibility
		// Add error handling to prevent panic from malformed regex (ReDoS prevention)
		re, err := regexp.Compile(`(?i)` + p.pattern)
		if err != nil {
			dd.logger.Warn("Failed to compile danger pattern - skipping",
				"pattern", p.pattern,
				"description", p.description,
				"error", err,
			)
			continue // Skip invalid patterns instead of panicking
		}
		dd.rules = append(dd.rules, &RegexRule{
			Pattern:     re,
			Description: p.description,
			Level:       p.level,
			Category:    p.category,
		})
	}
}

// loadSafePatterns initializes the built-in safe command patterns (allowlist).
// These patterns are checked first and will bypass the WAF if matched.
func (dd *Detector) loadSafePatterns() {
	patterns := []struct {
		pattern     string
		description string
		category    string
	}{
		// Go commands (Allowlist)
		{`^go\s+(build|run|test|vet|fmt|mod|get|install|list|version|env|tool|bug|clean|doc|generate|help|init|link)\b`, "Go build tool", "develop-tools"},
		{`^go\s+mod\s+(download|init|tidy|graph|why|verify)\b`, "Go mod command", "develop-tools"},

		// Node commands (Allowlist)
		{`^(npm|yarn|pnpm)\s+(install|run|test|build|start|dev|serve|lint|format|add|remove|update)\b`, "Node package manager", "develop-tools"},
		{`^node\s+[^\-]`, "Node.js runtime (safe invocation)", "develop-tools"},
		{`^npx\s+`, "Node package executor", "develop-tools"},

		// Python commands (Allowlist - only safe invocations)
		{`^python[23]?\s+(file\.py|script\.py|module| -m )\b`, "Python safe invocation", "develop-tools"},
		{`^pip[23]?\s+(install|uninstall|freeze|list|show|check)\b`, "Python pip", "develop-tools"},
		{`^poetry\s+(install|run|build|publish)\b`, "Python Poetry", "develop-tools"},
		{`^pipenv\s+(install|run|shell)\b`, "Python Pipenv", "develop-tools"},

		// Docker commands (Allowlist - specific safe operations)
		{`^docker\s+(build|ps|logs|images|volume|network|inspect|exec)\s+`, "Docker container", "develop-tools"},
		{`^docker-compose\s+`, "Docker Compose", "develop-tools"},

		// Git commands (Allowlist - safe operations only)
		{`^git\s+(status|log|diff|show|branch|checkout|fetch|pull|push|clone|init|add|commit|merge|rebase|stash|cherry-pick)\b`, "Git version control", "develop-tools"},

		// System tools (Allowlist - read-only/safe operations only)
		{`^(ls|cd|pwd|mkdir|rmdir|touch|head|tail|grep|find|awk|sed|sort|uniq|wc|cut|tr)\b`, "Unix utilities", "develop-tools"},
		{`^(date|time|which|whoami|id|hostname|uname|uptime)\b`, "System utilities", "develop-tools"},
	}

	for _, p := range patterns {
		re, err := regexp.Compile(p.pattern)
		if err != nil {
			dd.logger.Warn("Failed to compile safe pattern - skipping",
				"pattern", p.pattern,
				"description", p.description,
				"error", err)
			continue
		}
		dd.rules = append(dd.rules, &SafePatternRule{
			Pattern:     re,
			Description: p.description,
			Category:    p.category,
		})
	}
	dd.logger.Debug("Loaded safe command patterns", "count", len(patterns))
}

// stripMarkdownCodeBlocks removes markdown code blocks and inline code from input
// to reduce false positives when users paste documentation or code examples.
func stripMarkdownCodeBlocks(input string) string {
	// Pattern 1: Remove fenced code blocks (```...```)
	// Matches triple backticks with optional language identifier
	fencedCodeBlock := regexp.MustCompile("(?s)```[\\s\\S]*?```")
	input = fencedCodeBlock.ReplaceAllString(input, "")

	// Pattern 2: Remove indented code blocks (4+ spaces at line start)
	indentedBlock := regexp.MustCompile("(?m)^    .*$")
	input = indentedBlock.ReplaceAllString(input, "")

	// Note: We intentionally do NOT strip inline code (single backticks) here
	// because distinguishing between documentation (`code`) and actual commands (`whoami`)
	// is error-prone and could create security gaps.
	// The fenced and indented code blocks are clear documentation markers.

	return input
}

// CheckInput checks if the input contains any dangerous operations.
// Returns a DangerBlockEvent if a dangerous operation is detected, nil otherwise.
// Safe patterns are checked first - if matched, input is allowed through.
func (dd *Detector) CheckInput(input string) *DangerBlockEvent {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	// If bypass is enabled (admin/Evolution mode), skip checks
	if dd.bypassEnabled {
		dd.logger.Warn("Danger detection bypassed", "input", strutil.Truncate(input, MaxInputLogLength))
		dd.saveAuditEvent(input, nil, AuditActionBypassed)
		return nil
	}

	// Pre-process: Remove markdown code blocks to reduce false positives
	// This helps with prompts that contain documentation or examples
	input = stripMarkdownCodeBlocks(input)

	// Pre-check: Detect null bytes and dangerous control characters
	// These are often used to bypass regex-based WAF
	for i, r := range input {
		if r == '\x00' {
			dd.logger.Warn("Null byte detected in input - potential bypass attempt",
				"position", i,
				"input", strutil.Truncate(input, MaxPatternLogLength))
			block := &DangerBlockEvent{
				Operation:      strutil.Truncate(input, MaxDisplayLength),
				Reason:         "Null byte (\\x00) detected - potential WAF bypass attempt",
				PatternMatched: "null_byte",
				Level:          DangerLevelCritical,
				Category:       "injection",
				BypassAllowed:  false,
				Suggestions:    []string{"Remove null bytes from input", "Validate input encoding"},
			}
			dd.saveAuditEvent(input, block, AuditActionBlocked)
			return block
		}
		// Check for other dangerous control characters (except common whitespace)
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			dd.logger.Warn("Control character detected in input - potential bypass attempt",
				"position", i,
				"char_code", r,
				"input", strutil.Truncate(input, MaxPatternLogLength))
			block := &DangerBlockEvent{
				Operation:      strutil.Truncate(input, MaxDisplayLength),
				Reason:         fmt.Sprintf("Control character (0x%02X) detected - potential WAF bypass attempt", r),
				PatternMatched: "control_char",
				Level:          DangerLevelHigh,
				Category:       "injection",
				BypassAllowed:  false,
				Suggestions:    []string{"Remove control characters from input", "Validate input encoding"},
			}
			dd.saveAuditEvent(input, block, AuditActionBlocked)
			return block
		}
	}

	// First, check safe patterns (allowlist) - these bypass WAF
	for _, rule := range dd.rules {
		if safeRule, ok := rule.(*SafePatternRule); ok {
			if block := safeRule.Evaluate(input); block != nil {
				dd.logger.Info("Safe command detected - allowing",
					"reason", block.Reason,
					"category", block.Category,
					"input", strutil.Truncate(input, MaxPatternLogLength),
				)
				dd.saveAuditEvent(input, block, AuditActionApproved)
				return nil // Safe command - bypass WAF
			}
		}
	}

	// Then check dangerous patterns
	for _, rule := range dd.rules {
		// Skip safe pattern rules in dangerous check
		if _, ok := rule.(*SafePatternRule); ok {
			continue
		}
		if block := rule.Evaluate(input); block != nil {
			dd.logger.Warn("Dangerous operation detected",
				"reason", block.Reason,
				"level", block.Level,
				"category", block.Category,
				"input", strutil.Truncate(input, MaxPatternLogLength),
			)
			dd.saveAuditEvent(input, block, AuditActionBlocked)
			return block
		}
	}

	dd.saveAuditEvent(input, nil, AuditActionApproved)
	return nil
}

// saveAuditEvent saves an audit event to the audit store if configured.
func (dd *Detector) saveAuditEvent(input string, block *DangerBlockEvent, action AuditAction) {
	if dd.auditStore == nil {
		return
	}

	// Capture reference to avoid race condition
	store := dd.auditStore

	event := &AuditEvent{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Input:     strutil.Truncate(input, MaxInputLogLength),
		Action:    action,
		Source:    "detector",
	}

	if block != nil {
		event.Operation = block.Operation
		event.Reason = block.Reason
		event.Level = block.Level
		event.Category = block.Category
	}

	// Save asynchronously to avoid blocking
	go func() {
		if err := store.Save(context.Background(), event); err != nil {
			dd.logger.Error("Failed to save audit event", "error", err)
		}
	}()
}

// extractCommand extracts the relevant command portion from the input using pre-compiled regex.
func extractCommand(input string, pattern *regexp.Regexp) string {
	// Find the command line containing the dangerous pattern
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := scanner.Text()
		if pattern.MatchString(line) {
			// Truncate to reasonable length (use rune-aware truncateString for UTF-8 safety)
			return strutil.Truncate(line, MaxDisplayLength)
		}
	}
	// Fallback to truncated input (use existing truncateString from util.go)
	return strutil.Truncate(input, MaxDisplayLength)
}

var categorySuggestions = map[string][]string{
	"file_delete": {
		"Use 'rm -i' for interactive deletion with confirmation",
		"Consider moving files to a temporary backup directory first",
		"Use 'git status' to check what would be affected",
	},
	"git": {
		"Use 'git status' to check current changes",
		"Use 'git stash' to temporarily save changes",
		"Consider 'git checkout -- <file>' for single file recovery",
	},
	"network": {
		"Download scripts to a temp directory first for review",
		"Use 'curl -sL <url> | less' to inspect before executing",
		"Verify the source and checksum before execution",
	},
	"system": {
		"Ensure you have a recent backup before proceeding",
		"Test commands in a container or VM first",
		"Review the command documentation carefully",
	},
	"database": {
		"Use 'BEGIN; <query>; ROLLBACK;' to test first",
		"Create a database backup before running DDL/DML",
		"Use WHERE clause carefully to limit scope",
	},
	"injection": {
		"Avoid command substitution in user-provided input",
		"Use proper input validation and sanitization",
		"Consider using safer alternatives to eval/exec",
	},
	"privilege": {
		"Use least privilege principle",
		"Consider if the operation really needs elevated permissions",
		"Use sudo with specific command allowlist",
	},
	"persistence": {
		"Document any persistence mechanisms you add",
		"Consider temporary alternatives",
		"Review security implications of auto-start services",
	},
	"recon": {
		"Use authorized access methods only",
		"Log and audit sensitive file access",
		"Consider if the information is necessary for the task",
	},
	"container": {
		"Avoid privileged containers in production",
		"Use dedicated security contexts",
		"Review container security best practices",
	},
	"kernel": {
		"Kernel module changes require extreme caution",
		"Use signed kernel modules when possible",
		"Document and audit any kernel modifications",
	},
}

var defaultSuggestions = []string{
	"Consider if there's a safer alternative",
	"Ensure you have a backup before proceeding",
}

func getSuggestions(category string) []string {
	if suggestions, ok := categorySuggestions[category]; ok {
		return suggestions
	}
	return defaultSuggestions
}

// SetAllowPaths sets the list of allowed safe paths.
// Paths are cleaned to eliminate arbitrary trailing slashes or relative segments.
func (dd *Detector) SetAllowPaths(paths []string) {
	dd.mu.Lock()
	defer dd.mu.Unlock()
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned = append(cleaned, filepath.Clean(p))
	}
	dd.allowPaths = cleaned
	dd.logger.Debug("Danger detector allow paths updated", "paths", cleaned)
}

// SetBypassEnabled enables or disables bypass mode.
// Requires a valid admin token to succeed.
// When enabled, dangerous operations are NOT blocked (admin/Evolution mode only).
func (dd *Detector) SetBypassEnabled(token string, enabled bool) error {
	dd.mu.Lock()
	defer dd.mu.Unlock()

	// Constant-time comparison to prevent timing attacks
	if dd.adminToken == "" {
		dd.logger.Error("Bypass toggle attempted but no admin token is configured")
		return fmt.Errorf("security: admin token not configured")
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(dd.adminToken)) != 1 {
		dd.logger.Warn("Unauthorized bypass toggle attempt", "enabled", enabled)
		return fmt.Errorf("security: unauthorized bypass toggle attempt")
	}

	dd.bypassEnabled = enabled
	if enabled {
		dd.logger.Warn("AUDIT: Danger detector bypass ENABLED by administrator")
	} else {
		dd.logger.Info("AUDIT: Danger detector bypass disabled by administrator")
	}
	return nil
}

// IsPathAllowed checks if a path is in the allowlist.
// Both the input path and allowed paths should be cleaned first.
func (dd *Detector) IsPathAllowed(path string) bool {
	dd.mu.RLock()
	defer dd.mu.RUnlock()

	cleanPath := filepath.Clean(path)

	for _, allowed := range dd.allowPaths {
		// Exact match
		if cleanPath == allowed {
			return true
		}
		// Subdirectory match - must end with separator to prevent prefix hijacking
		// e.g. allowed="/opt/dir", path="/opt/dir-malicious" should be blocked
		if strings.HasPrefix(cleanPath, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// CheckFileAccess checks if file access is within allowed paths.
// Returns true if the access is safe (within allowed paths), false otherwise.
func (dd *Detector) CheckFileAccess(filePath string) bool {
	// Clean the path and expand env vars
	filePath = os.ExpandEnv(filePath)
	if !filepath.IsAbs(filePath) {
		// Relative path - resolve to absolute location
		cwd, err := os.Getwd()
		if err == nil {
			filePath = filepath.Join(cwd, filePath)
		}
	}

	filePath = filepath.Clean(filePath)

	return dd.IsPathAllowed(filePath)
}

// LoadCustomPatterns loads custom danger patterns from a file.
// File format: one pattern per line: "regex|description|level|category"
func (dd *Detector) LoadCustomPatterns(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open custom patterns file: %w", err)
	}
	defer func() { _ = file.Close() }() //nolint:errcheck // file cleanup

	dd.mu.Lock()
	defer dd.mu.Unlock()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			dd.logger.Warn("Invalid custom pattern format", "line", lineNum, "content", line)
			continue
		}

		re, err := regexp.Compile(`(?i)` + parts[0])
		if err != nil {
			dd.logger.Warn("Failed to compile pattern", "line", lineNum, "pattern", parts[0], "error", err)
			continue
		}

		var level DangerLevel
		switch parts[2] {
		case "critical":
			level = DangerLevelCritical
		case "high":
			level = DangerLevelHigh
		case "moderate":
			level = DangerLevelModerate
		default:
			level = DangerLevelModerate
		}

		dd.rules = append(dd.rules, &RegexRule{
			Pattern:     re,
			Description: parts[1],
			Level:       level,
			Category:    parts[3],
		})
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading patterns file: %w", err)
	}

	dd.logger.Info("Loaded custom danger patterns", "file", filename)
	return nil
}

// String returns a string representation of the danger level.
func (d DangerLevel) String() string {
	switch d {
	case DangerLevelSafe:
		return "safe"
	case DangerLevelCritical:
		return "critical"
	case DangerLevelHigh:
		return "high"
	case DangerLevelModerate:
		return "moderate"
	default:
		return "unknown"
	}
}
