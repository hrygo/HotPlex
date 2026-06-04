package brain

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// ErrBrainNotConfigured is returned when a brain feature is used without a configured LLM backend.
var ErrBrainNotConfigured = errors.New("brain not configured")

// Compile-time interface verification
var _ Brain = (*enhancedBrainWrapper)(nil)

// ChatOptions controls LLM generation parameters.
// Re-exported from llm package for convenience.
type ChatOptions = llm.ChatOptions

// Brain represents the core intelligence for HotPlex.
// It provides lightweight LLM capabilities (e.g. TTS message summarization).
type Brain interface {
	// Chat generates a plain text response for a given prompt.
	Chat(ctx context.Context, prompt string) (string, error)

	// ChatWithOptions generates a response with fine-grained control over LLM parameters.
	ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error)
}

var (
	globalBrainMu sync.RWMutex
	globalBrain   Brain
)

// Global returns the globally configured Brain instance.
// If no brain is configured, it returns nil.
func Global() Brain {
	globalBrainMu.RLock()
	defer globalBrainMu.RUnlock()
	return globalBrain
}

// SetGlobal sets the global Brain instance.
// If the new brain implements io.Closer, SetGlobal calls Close on the
// *previous* instance before replacing it — this prevents resource leaks
// (e.g. goroutine leaks from RateLimiter) on hot-reload.
func SetGlobal(b Brain) {
	globalBrainMu.Lock()
	prev := globalBrain
	globalBrain = b
	globalBrainMu.Unlock()

	// Close outside the lock — it may block waiting for goroutines to exit.
	if prev != nil {
		if c, ok := prev.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

// Close shuts down the global Brain and releases resources.
// Safe to call even if Brain was never initialized.
func Close() { SetGlobal(nil) }
