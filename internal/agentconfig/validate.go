package agentconfig

import (
	"fmt"
	"path/filepath"
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
