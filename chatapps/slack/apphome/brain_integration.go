package apphome

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hrygo/hotplex/brain"
)

// ErrIntentNotConfirmed is returned when the user doesn't confirm the intent.
var ErrIntentNotConfirmed = fmt.Errorf("intent not confirmed")

// BrainIntegration provides Native Brain integration for capabilities.
type BrainIntegration struct {
	brain  brain.Brain
	logger *slog.Logger
}

// NewBrainIntegration creates a new Brain integration.
func NewBrainIntegration(b brain.Brain) *BrainIntegration {
	return &BrainIntegration{
		brain:  b,
		logger: slog.Default(),
	}
}

// PreparePrompt prepares the final prompt with Brain preprocessing.
func (bi *BrainIntegration) PreparePrompt(
	ctx context.Context,
	cap Capability,
	params map[string]string,
	basePrompt string,
) (string, error) {
	prompt := basePrompt

	// Step 1: Intent confirmation (optional)
	if cap.BrainOpts.IntentConfirm {
		confirmed, err := bi.confirmIntent(ctx, prompt)
		if err != nil {
			bi.logger.Warn("Intent confirmation failed",
				"capability", cap.ID,
				"error", err)
			// Non-fatal: continue without confirmation
		} else if !confirmed {
			return "", ErrIntentNotConfirmed
		}
	}

	// Step 2: Context compression (optional)
	if cap.BrainOpts.CompressContext {
		compressed, err := bi.compressContext(ctx, prompt)
		if err != nil {
			bi.logger.Warn("Context compression failed",
				"capability", cap.ID,
				"error", err)
			// Non-fatal: continue with original prompt
		} else {
			prompt = compressed
		}
	}

	return prompt, nil
}

// confirmIntent uses Brain to confirm the user's intent.
func (bi *BrainIntegration) confirmIntent(ctx context.Context, prompt string) (bool, error) {
	if bi.brain == nil {
		return true, nil // No brain, assume confirmed
	}

	// Ask Brain to confirm the intent
	confirmPrompt := fmt.Sprintf(
		"Analyze the following prompt and determine if it is clear and safe to execute. "+
			"Respond with only 'yes' or 'no'.\n\nPrompt:\n%s", prompt)

	response, err := bi.brain.Chat(ctx, confirmPrompt)
	if err != nil {
		return false, fmt.Errorf("brain chat: %w", err)
	}

	// Parse response (case-insensitive match)
	bi.logger.Debug("Intent confirmation response", "response", response)
	return strings.EqualFold(strings.TrimSpace(response), "yes"), nil
}

// compressContext uses Brain to compress the context.
func (bi *BrainIntegration) compressContext(ctx context.Context, prompt string) (string, error) {
	if bi.brain == nil {
		return prompt, nil // No brain, return original
	}

	// Ask Brain to compress the prompt
	compressPrompt := fmt.Sprintf(
		"Compress the following prompt while preserving all essential information. "+
			"Remove redundancy but keep the key instructions.\n\nPrompt:\n%s", prompt)

	compressed, err := bi.brain.Chat(ctx, compressPrompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}

	bi.logger.Debug("Context compressed",
		"original_length", len(prompt),
		"compressed_length", len(compressed))

	return compressed, nil
}

// EnhancePrompt uses Brain to enhance a prompt with additional context.
func (bi *BrainIntegration) EnhancePrompt(ctx context.Context, prompt string, context string) (string, error) {
	if bi.brain == nil {
		return prompt, nil
	}

	enhancePrompt := fmt.Sprintf(
		"Enhance the following prompt with the given context. "+
			"Keep the original intent but add relevant context.\n\nOriginal prompt:\n%s\n\nContext:\n%s",
		prompt, context)

	enhanced, err := bi.brain.Chat(ctx, enhancePrompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}

	return enhanced, nil
}

// SetLogger sets the logger.
func (bi *BrainIntegration) SetLogger(logger *slog.Logger) {
	bi.logger = logger
}
