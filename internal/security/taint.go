package security

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/internal/strutil"
)

// TaintSource identifies the source of taint in the system.
type TaintSource string

const (
	// TaintSourceUserInput indicates data came from user input.
	TaintSourceUserInput TaintSource = "user_input"
	// TaintSourceFile indicates data came from file reading.
	TaintSourceFile TaintSource = "file"
	// TaintSourceNetwork indicates data came from network.
	TaintSourceNetwork TaintSource = "network"
	// TaintSourceEnvironment indicates data came from environment variables.
	TaintSourceEnvironment TaintSource = "environment"
	// TaintSourceCommand indicates data came from command execution.
	TaintSourceCommand TaintSource = "command"
	// TaintSourceDatabase indicates data came from database query.
	TaintSourceDatabase TaintSource = "database"
	// TaintSourceAPI indicates data came from external API.
	TaintSourceAPI TaintSource = "api"

	// TaintSafe indicates the data is considered safe (after sanitization).
	TaintSafe TaintSource = "safe"
)

// TaintLevel represents the level of taint.
type TaintLevel int

const (
	// TaintLevelUntrusted indicates data from untrusted source.
	TaintLevelUntrusted TaintLevel = iota
	// TaintLevelExternal indicates external data that needs validation.
	TaintLevelExternal
	// TaintLevelValidated indicates validated external data.
	TaintLevelValidated
	// TaintLevelSanitized indicates data that has been sanitized.
	TaintLevelSanitized
	// TaintLevelTrusted indicates data from trusted source.
	TaintLevelTrusted
)

// TaintTag represents metadata about the taint.
type TaintTag struct {
	// Source identifies where the data originated.
	Source TaintSource

	// Level indicates the current trust level.
	Level TaintLevel

	// Path tracks the data flow path (for debugging).
	Path []string

	// Operations applied to this data.
	Operations []string

	// Metadata contains additional information.
	Metadata map[string]any

	// CreatedAt indicates when this taint was created.
	// Timestamp is tracked via the Taint itself.
}

// Taint represents taint tracking for data flow security.
type Taint struct {
	Value    string
	Tag      TaintTag
	ID       string
	ParentID string
}

// TaintTracker tracks taint propagation through data flows.
type TaintTracker struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// activeTaints stores currently tracked taints.
	activeTaints map[string]*Taint

	// taintHistory stores taint history for auditing.
	taintHistory []*Taint

	// maxHistory is the maximum history to keep.
	maxHistory int

	// sanitizer is the sanitizer function.
	sanitizer func(string) string

	// validator is the validator function.
	validator func(string) (bool, string)

	// hooks are called when taint state changes.
	hooks []TaintHook
}

// TaintHook defines a function to be called on taint events.
type TaintHook func(event TaintEvent)

// TaintEvent represents a taint-related event.
type TaintEvent struct {
	Type      string
	TaintID   string
	Operation string
	Details   string
}

// NewTaintTracker creates a new TaintTracker instance.
func NewTaintTracker(logger *slog.Logger) *TaintTracker {
	tt := &TaintTracker{
		logger:       logger,
		activeTaints: make(map[string]*Taint),
		maxHistory:   1000,
	}

	if tt.logger == nil {
		tt.logger = slog.Default()
	}

	return tt
}

// SetSanitizer sets the sanitization function.
func (tt *TaintTracker) SetSanitizer(sanitizer func(string) string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.sanitizer = sanitizer
}

// SetValidator sets the validation function.
func (tt *TaintTracker) SetValidator(validator func(string) (bool, string)) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.validator = validator
}

// AddHook adds a taint event hook.
func (tt *TaintTracker) AddHook(hook TaintHook) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.hooks = append(tt.hooks, hook)
}

// MarkUntrusted marks a string as untrusted from a specific source.
func (tt *TaintTracker) MarkUntrusted(value string, source TaintSource) *Taint {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	taint := &Taint{
		Value: value,
		Tag: TaintTag{
			Source:   source,
			Level:    TaintLevelUntrusted,
			Path:     []string{string(source)},
			Metadata: make(map[string]any),
		},
		ID: generateTaintID(),
	}

	tt.activeTaints[taint.ID] = taint
	tt.taintHistory = append(tt.taintHistory, taint)
	tt.pruneHistory()

	tt.emitHook(TaintEvent{
		Type:    "marked_untrusted",
		TaintID: taint.ID,
		Details: fmt.Sprintf("source=%s", source),
	})

	tt.logger.Debug("Marked untrusted",
		"id", taint.ID,
		"source", source,
		"length", len(value))

	return taint
}

// MarkTrusted marks a string as trusted.
func (tt *TaintTracker) MarkTrusted(value string) *Taint {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	taint := &Taint{
		Value: value,
		Tag: TaintTag{
			Source:   TaintSafe,
			Level:    TaintLevelTrusted,
			Path:     []string{"trusted"},
			Metadata: make(map[string]any),
		},
		ID: generateTaintID(),
	}

	tt.activeTaints[taint.ID] = taint
	tt.taintHistory = append(tt.taintHistory, taint)
	tt.pruneHistory()

	return taint
}

// Get returns a taint by ID.
func (tt *TaintTracker) Get(id string) (*Taint, bool) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	taint, ok := tt.activeTaints[id]
	return taint, ok
}

// GetValue retrieves the value from a taint, applying sanitization if needed.
func (tt *TaintTracker) GetValue(id string) (string, error) {
	tt.mu.RLock()
	taint, ok := tt.activeTaints[id]
	sanitizer := tt.sanitizer
	tt.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("taint not found: %s", id)
	}

	value := taint.Value

	// Apply sanitizer if the taint level requires it
	if taint.Tag.Level < TaintLevelSanitized && sanitizer != nil {
		value = sanitizer(value)
		tt.mu.Lock()
		taint.Tag.Operations = append(taint.Tag.Operations, "sanitized")
		taint.Tag.Level = TaintLevelSanitized
		tt.mu.Unlock()
	}

	return value, nil
}

// Propagate propagates taint to a new value (e.g., from string manipulation).
func (tt *TaintTracker) Propagate(parentID string, newValue string, operation string) (*Taint, error) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	parent, ok := tt.activeTaints[parentID]
	if !ok {
		return nil, fmt.Errorf("parent taint not found: %s", parentID)
	}

	child := &Taint{
		Value: newValue,
		Tag: TaintTag{
			Source:     parent.Tag.Source,
			Level:      parent.Tag.Level,
			Path:       append([]string{}, parent.Tag.Path...),
			Operations: append([]string{}, parent.Tag.Operations...),
			Metadata:   make(map[string]any),
		},
		ID:       generateTaintID(),
		ParentID: parentID,
	}

	// Add operation to path
	child.Tag.Path = append(child.Tag.Path, operation)
	child.Tag.Operations = append(child.Tag.Operations, operation)

	// Copy metadata
	for k, v := range parent.Tag.Metadata {
		child.Tag.Metadata[k] = v
	}

	tt.activeTaints[child.ID] = child
	tt.taintHistory = append(tt.taintHistory, child)
	tt.pruneHistory()

	tt.emitHookLocked(TaintEvent{
		Type:      "propagated",
		TaintID:   child.ID,
		Operation: operation,
		Details:   fmt.Sprintf("parent=%s", parentID),
	})

	tt.logger.Debug("Propagated taint",
		"child_id", child.ID,
		"parent_id", parentID,
		"operation", operation)

	return child, nil
}

// Validate checks if a taint meets validation criteria.
func (tt *TaintTracker) Validate(id string) (bool, string) {
	tt.mu.RLock()
	taint, ok := tt.activeTaints[id]
	validator := tt.validator
	tt.mu.RUnlock()

	if !ok {
		return false, "taint not found"
	}

	// If already trusted, no need to validate
	if taint.Tag.Level >= TaintLevelValidated {
		return true, ""
	}

	// Use custom validator if provided
	if validator != nil {
		valid, reason := validator(taint.Value)
		if valid {
			tt.mu.Lock()
			taint.Tag.Level = TaintLevelValidated
			taint.Tag.Operations = append(taint.Tag.Operations, "validated")
			tt.mu.Unlock()
		}
		return valid, reason
	}

	// Default validation: check if not empty and no obvious injection patterns
	if len(taint.Value) == 0 {
		return false, "empty value"
	}

	// Basic injection pattern check
	dangerPatterns := []string{
		"$(", "`", "${", "; rm", "| rm", "&& rm",
		"../", "..\\", "%2e%2e",
		" DROP ", " DELETE ", " INSERT ", " UPDATE ",
	}

	lowerValue := strings.ToLower(taint.Value)
	for _, pattern := range dangerPatterns {
		if strings.Contains(lowerValue, strings.ToLower(pattern)) {
			return false, fmt.Sprintf("dangerous pattern detected: %s", pattern)
		}
	}

	// Mark as validated
	tt.mu.Lock()
	taint.Tag.Level = TaintLevelValidated
	taint.Tag.Operations = append(taint.Tag.Operations, "validated")
	tt.mu.Unlock()

	return true, ""
}

// Sanitize sanitizes a taint's value.
func (tt *TaintTracker) Sanitize(id string) (string, error) {
	tt.mu.RLock()
	taint, ok := tt.activeTaints[id]
	sanitizer := tt.sanitizer
	tt.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("taint not found: %s", id)
	}

	if sanitizer == nil {
		return defaultSanitize(taint.Value), nil
	}

	sanitized := sanitizer(taint.Value)

	tt.mu.Lock()
	taint.Value = sanitized
	taint.Tag.Level = TaintLevelSanitized
	taint.Tag.Operations = append(taint.Tag.Operations, "sanitized")
	tt.mu.Unlock()

	return sanitized, nil
}

// Clear removes a taint from tracking.
func (tt *TaintTracker) Clear(id string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if _, ok := tt.activeTaints[id]; ok {
		delete(tt.activeTaints, id)
		tt.emitHookLocked(TaintEvent{
			Type:    "cleared",
			TaintID: id,
		})
	}
}

// GetTaintInfo returns information about a taint.
func (tt *TaintTracker) GetTaintInfo(id string) (map[string]any, bool) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	taint, ok := tt.activeTaints[id]
	if !ok {
		return nil, false
	}

	return map[string]any{
		"id":         taint.ID,
		"parent_id":  taint.ParentID,
		"value":      strutil.Truncate(taint.Value, 100),
		"source":     taint.Tag.Source,
		"level":      taint.Tag.Level.String(),
		"path":       taint.Tag.Path,
		"operations": taint.Tag.Operations,
		"metadata":   taint.Tag.Metadata,
	}, true
}

// ListActive returns all active taints.
func (tt *TaintTracker) ListActive() []*Taint {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	taints := make([]*Taint, 0, len(tt.activeTaints))
	for _, t := range tt.activeTaints {
		taints = append(taints, t)
	}
	return taints
}

// GetStatistics returns statistics about taint tracking.
func (tt *TaintTracker) GetStatistics() map[string]any {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	stats := map[string]any{
		"active_count":   len(tt.activeTaints),
		"history_count":  len(tt.taintHistory),
		"max_history":    tt.maxHistory,
	}

	// Count by level
	levelCounts := make(map[string]int)
	levelNames := []string{"untrusted", "external", "validated", "sanitized", "trusted"}
	for _, t := range tt.activeTaints {
		if int(t.Tag.Level) < len(levelNames) {
			levelCounts[levelNames[t.Tag.Level]]++
		}
	}
	stats["by_level"] = levelCounts

	// Count by source
	sourceCounts := make(map[string]int)
	for _, t := range tt.activeTaints {
		sourceCounts[string(t.Tag.Source)]++
	}
	stats["by_source"] = sourceCounts

	return stats
}

// emitHook emits a taint event to all hooks.
func (tt *TaintTracker) emitHook(event TaintEvent) {
	tt.mu.Lock()
	hooks := make([]TaintHook, len(tt.hooks))
	copy(hooks, tt.hooks)
	tt.mu.Unlock()

	for _, hook := range hooks {
		hook(event)
	}
}

// emitHookLocked emits a taint event (must be called with lock held).
func (tt *TaintTracker) emitHookLocked(event TaintEvent) {
	hooks := make([]TaintHook, len(tt.hooks))
	copy(hooks, tt.hooks)

	for _, hook := range hooks {
		hook(event)
	}
}

// pruneHistory removes old entries from history.
func (tt *TaintTracker) pruneHistory() {
	if len(tt.taintHistory) > tt.maxHistory {
		tt.taintHistory = tt.taintHistory[len(tt.taintHistory)-tt.maxHistory:]
	}
}

// String returns a string representation of TaintLevel.
func (tl TaintLevel) String() string {
	switch tl {
	case TaintLevelUntrusted:
		return "untrusted"
	case TaintLevelExternal:
		return "external"
	case TaintLevelValidated:
		return "validated"
	case TaintLevelSanitized:
		return "sanitized"
	case TaintLevelTrusted:
		return "trusted"
	default:
		return "unknown"
	}
}

// generateTaintID generates a unique taint ID.
func generateTaintID() string {
	return fmt.Sprintf("t_%d_%d", activeTaintCount, getTaintCounter())
}

var taintCounter uint64
var activeTaintCount int

func getTaintCounter() uint64 {
	taintCounter++
	return taintCounter
}

// defaultSanitize provides default sanitization for untrusted strings.
func defaultSanitize(value string) string {
	// Remove null bytes
	value = strings.ReplaceAll(value, "\x00", "")

	// Escape HTML entities
	value = escapeHTML(value)

	// Remove or escape shell metacharacters
	value = escapeShellMetacharacters(value)

	// Normalize whitespace
	value = normalizeWhitespaceStr(value)

	return value
}

// escapeShellMetacharacters escapes dangerous shell characters.
func escapeShellMetacharacters(value string) string {
	dangerous := []string{
		";", "&", "|", "`", "$", "(", ")", "<", ">", "\n", "\r",
	}

	result := value
	for _, char := range dangerous {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}

	return result
}

// escapeHTML escapes HTML entities.
func escapeHTML(value string) string {
	var sb strings.Builder
	sb.Grow(len(value) * 2)

	for _, r := range value {
		switch r {
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '&':
			sb.WriteString("&amp;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#39;")
		default:
			sb.WriteRune(r)
		}
	}

	return sb.String()
}

// normalizeWhitespaceStr normalizes whitespace in the string.
func normalizeWhitespaceStr(value string) string {
	result := strings.Fields(value)
	return strings.Join(result, " ")
}

// CommandTaintChecker checks if a command contains tainted data.
type CommandTaintChecker struct {
	tracker *TaintTracker
	logger  *slog.Logger
}

// NewCommandTaintChecker creates a new CommandTaintChecker.
func NewCommandTaintChecker(tracker *TaintTracker, logger *slog.Logger) *CommandTaintChecker {
	return &CommandTaintChecker{
		tracker: tracker,
		logger:  logger,
	}
}

// CheckCommandTaint checks a command for tainted arguments.
func (ctc *CommandTaintChecker) CheckCommandTaint(cmd string, taintIDs []string) (bool, string) {
	for _, id := range taintIDs {
		taint, ok := ctc.tracker.Get(id)
		if !ok {
			continue
		}

		// Check if tainted value appears in command
		if strings.Contains(cmd, taint.Value) {
			if ctc.logger != nil {
				ctc.logger.Warn("Tainted data found in command",
					"command", strutil.Truncate(cmd, 50),
					"taint_id", id,
					"source", taint.Tag.Source,
					"level", taint.Tag.Level)
			}

			// If the taint is not at least validated, block
			if taint.Tag.Level < TaintLevelValidated {
				return false, fmt.Sprintf("unvalidated taint in command (source: %s)", taint.Tag.Source)
			}
		}
	}

	return true, ""
}

// SQLInjectionGuard provides SQL injection protection through taint tracking.
type SQLInjectionGuard struct {
	tracker *TaintTracker
	logger  *slog.Logger
}

// NewSQLInjectionGuard creates a new SQLInjectionGuard.
func NewSQLInjectionGuard(tracker *TaintTracker, logger *slog.Logger) *SQLInjectionGuard {
	return &SQLInjectionGuard{
		tracker: tracker,
		logger:  logger,
	}
}

// CheckQuery checks a SQL query for taint-based SQL injection vulnerabilities.
func (sig *SQLInjectionGuard) CheckQuery(query string, taintIDs []string) (bool, string) {
	dangerPatterns := []struct {
		pattern string
		desc    string
	}{
		{"';", "single quote termination"},
		{"--", "comment"},
		{"/*", "block comment"},
		{"UNION", "UNION injection"},
		{"DROP ", "DROP statement"},
		{"DELETE ", "DELETE statement"},
		{"INSERT ", "INSERT statement"},
		{"UPDATE ", "UPDATE statement"},
		{"EXEC ", "EXEC statement"},
		{"xp_", "extended stored procedure"},
	}

	for _, id := range taintIDs {
		taint, ok := sig.tracker.Get(id)
		if !ok {
			continue
		}

		upperValue := strings.ToUpper(taint.Value)
		for _, pattern := range dangerPatterns {
			if strings.Contains(upperValue, strings.ToUpper(pattern.pattern)) {
				if taint.Tag.Level < TaintLevelValidated {
					return false, fmt.Sprintf("potential SQL injection: %s in taint from %s",
						pattern.desc, taint.Tag.Source)
				}
			}
		}
	}

	return true, ""
}
