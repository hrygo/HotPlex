package apphome

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/hrygo/hotplex/brain"
	"github.com/slack-go/slack"
)

// Executor executes capabilities with the given parameters.
type Executor struct {
	client           *slack.Client
	brain            brain.Brain
	brainIntegration *BrainIntegration
	logger           *slog.Logger

	// MessageHandler is called to trigger engine execution.
	// This is a callback to avoid circular dependencies with the adapter.
	MessageHandler func(ctx context.Context, userID, channelID, message string) error
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithExecutorClient sets the Slack client.
func WithExecutorClient(client *slack.Client) ExecutorOption {
	return func(e *Executor) {
		e.client = client
	}
}

// WithBrain sets the Brain instance.
func WithBrain(b brain.Brain) ExecutorOption {
	return func(e *Executor) {
		e.brain = b
		e.brainIntegration = NewBrainIntegration(b)
	}
}

// WithMessageHandler sets the message handler callback.
func WithMessageHandler(handler func(ctx context.Context, userID, channelID, message string) error) ExecutorOption {
	return func(e *Executor) {
		e.MessageHandler = handler
	}
}

// WithExecutorLogger sets the logger.
func WithExecutorLogger(logger *slog.Logger) ExecutorOption {
	return func(e *Executor) {
		e.logger = logger
	}
}

// NewExecutor creates a new capability executor.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Execute executes a capability with the given parameters.
func (e *Executor) Execute(ctx context.Context, userID string, cap Capability, params map[string]string) error {
	startTime := time.Now()

	e.logger.Info("Executing capability",
		"user", userID,
		"capability", cap.ID,
		"params", params)

	// Step 1: Render the prompt template
	prompt, err := e.renderPrompt(cap, params)
	if err != nil {
		return fmt.Errorf("render prompt: %w", err)
	}

	e.logger.Debug("Rendered prompt", "prompt_length", len(prompt))

	// Step 2: Apply Brain preprocessing if available
	if e.brainIntegration != nil {
		processedPrompt, err := e.brainIntegration.PreparePrompt(ctx, cap, params, prompt)
		if err != nil {
			e.logger.Warn("Brain preprocessing failed, using original prompt",
				"error", err)
		} else {
			prompt = processedPrompt
		}
	}

	// Step 3: Get or create DM channel
	dmChannel, err := e.getOrCreateDMChannel(ctx, userID)
	if err != nil {
		return fmt.Errorf("get DM channel: %w", err)
	}

	// Step 4: Send header message to DM
	header := fmt.Sprintf("🎯 *%s* — 执行能力任务", cap.Name)
	if _, _, err := e.client.PostMessageContext(
		ctx,
		dmChannel,
		slack.MsgOptionText(header, false),
	); err != nil {
		e.logger.Error("Failed to send header message", "error", err)
	}

	// Step 5: Send the rendered prompt to DM
	if _, _, err := e.client.PostMessageContext(
		ctx,
		dmChannel,
		slack.MsgOptionText(prompt, false),
	); err != nil {
		return fmt.Errorf("send prompt to DM: %w", err)
	}

	// Step 6: Trigger engine execution via callback
	if e.MessageHandler != nil {
		if err := e.MessageHandler(ctx, userID, dmChannel, prompt); err != nil {
			e.logger.Error("Message handler failed", "error", err)
			// Don't fail the execution, the prompt was already sent
		}
	}

	duration := time.Since(startTime)
	e.logger.Info("Capability execution completed",
		"user", userID,
		"capability", cap.ID,
		"duration", duration)

	return nil
}

// renderPrompt renders the capability's prompt template with the given parameters.
func (e *Executor) renderPrompt(cap Capability, params map[string]string) (string, error) {
	tmpl, err := template.New(cap.ID).Parse(cap.PromptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// getOrCreateDMChannel gets or creates a DM channel with the user.
func (e *Executor) getOrCreateDMChannel(ctx context.Context, userID string) (string, error) {
	if e.client == nil {
		return "", fmt.Errorf("slack client not configured")
	}

	// Open or resume a DM conversation with the user
	channel, _, _, err := e.client.OpenConversationContext(ctx, &slack.OpenConversationParameters{
		Users: []string{userID},
	})
	if err != nil {
		return "", fmt.Errorf("open conversation: %w", err)
	}

	return channel.ID, nil
}

// SetClient sets the Slack client (for late initialization).
func (e *Executor) SetClient(client *slack.Client) {
	e.client = client
}

// SetBrain sets the Brain instance (for late initialization).
func (e *Executor) SetBrain(b brain.Brain) {
	e.brain = b
	e.brainIntegration = NewBrainIntegration(b)
}
