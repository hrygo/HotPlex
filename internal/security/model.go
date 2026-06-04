package security

import (
	"fmt"
	"strings"
	"sync"
)

var (
	modelsMu      sync.RWMutex
	allowedModels = map[string]bool{
		// Claude models.
		"claude-sonnet-4-6":          true,
		"claude-opus-4-6":            true,
		"claude-3-5-sonnet-20241022": true,
		"claude-3-5-haiku-20241022":  true,
		"claude-3-opus-20240229":     true,
		"claude-3-sonnet-20240229":   true,
	}
)

// ValidateModel checks that the model identifier is in the allowed list.
func ValidateModel(model string) error {
	if model == "" {
		return fmt.Errorf("security: empty model name")
	}
	lower := strings.ToLower(model)
	modelsMu.RLock()
	ok := allowedModels[lower]
	modelsMu.RUnlock()
	if !ok {
		return fmt.Errorf("security: model %q not in allowed list", model)
	}
	return nil
}

// IsModelAllowed returns true if the model is permitted.
func IsModelAllowed(model string) bool {
	modelsMu.RLock()
	ok := allowedModels[strings.ToLower(model)]
	modelsMu.RUnlock()
	return ok
}

// RegisterModel adds a model to the allowed list.
func RegisterModel(model string) {
	modelsMu.Lock()
	allowedModels[strings.ToLower(model)] = true
	modelsMu.Unlock()
}
