package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// Sentinel errors for agent-config override validation (spec ② §10).
var (
	ErrInvalidConfigJSON  = errors.New("agentconfig: invalid config JSON")
	ErrUnknownConfigFile  = errors.New("agentconfig: unknown config file")
	ErrConfigTooLarge     = errors.New("agentconfig: config exceeds size limit")
	ErrInvalidConfigValue = errors.New("agentconfig: invalid config value")
)

// ValidateBotName checks that name contains no path separators or traversal
// components ("." or ".."). It returns ErrInvalidBotName when the check fails.
// An empty name is allowed (single-bot mode skips bot-level lookup).
func ValidateBotName(name string) error {
	if name == "" {
		return nil
	}
	if filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("%w: %q: path traversal not allowed", ErrInvalidBotName, name)
	}
	return nil
}

// ValidateOverrides parses raw JSON into a flat map of config-file-name → content
// and validates keys (against configFiles), value types (must be string), and size
// limits (MaxFileChars per file, MaxTotalChars total). Returns the parsed map on
// success. Empty raw ("") returns (nil, nil) meaning "no overrides".
//
// Used by the workspace PATCH handler (write-time validation) and Bridge's
// resolveWorkspaceOverrides (read-time parsing). META-COGNITION.md is not in
// configFiles, so it is rejected here — workspace overrides cannot touch it.
func ValidateOverrides(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfigJSON, err)
	}
	known := make(map[string]struct{}, len(configFiles))
	for _, f := range configFiles {
		known[f] = struct{}{}
	}
	out := make(map[string]string, len(m))
	var total int
	for k, v := range m {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownConfigFile, k)
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %q must be a string", ErrInvalidConfigValue, k)
		}
		if len(s) > MaxFileChars {
			return nil, fmt.Errorf("%w: %q is %d chars, max %d", ErrConfigTooLarge, k, len(s), MaxFileChars)
		}
		total += len(s)
		out[k] = s
	}
	if total > MaxTotalChars {
		return nil, fmt.Errorf("%w: total %d chars exceeds max %d", ErrConfigTooLarge, total, MaxTotalChars)
	}
	return out, nil
}
