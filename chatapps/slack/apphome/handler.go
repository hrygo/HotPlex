package apphome

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/slack-go/slack"
)

// Handler handles App Home events and interactions.
type Handler struct {
	client   *slack.Client
	registry *Registry
	builder  *Builder
	form     *FormBuilder
	executor *Executor
	logger   *slog.Logger
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithSlackClient sets the Slack client.
func WithSlackClient(client *slack.Client) HandlerOption {
	return func(h *Handler) {
		h.client = client
	}
}

// WithExecutor sets the capability executor.
func WithExecutor(executor *Executor) HandlerOption {
	return func(h *Handler) {
		h.executor = executor
	}
}

// WithHandlerLogger sets the logger.
func WithHandlerLogger(logger *slog.Logger) HandlerOption {
	return func(h *Handler) {
		h.logger = logger
	}
}

// NewHandler creates a new App Home handler.
func NewHandler(registry *Registry, opts ...HandlerOption) *Handler {
	h := &Handler{
		registry: registry,
		builder:  NewBuilder(registry),
		form:     &FormBuilder{},
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// HomeOpenedEvent represents the app_home_opened event data.
// This is a simplified version for our use case.
type HomeOpenedEvent struct {
	User    string
	Channel string
	Tab     string
}

// HandleHomeOpened handles the app_home_opened event.
// This is called when a user opens the App Home tab.
func (h *Handler) HandleHomeOpened(ctx context.Context, event *HomeOpenedEvent) error {
	if h.client == nil {
		return fmt.Errorf("slack client not configured")
	}

	h.logger.Debug("Handling app_home_opened event",
		"user", event.User,
		"channel", event.Channel,
		"tab", event.Tab)

	// Build the Home Tab view
	view := h.builder.BuildFullHomeView()

	// Publish the view
	_, err := h.client.PublishViewContext(
		ctx,
		slack.PublishViewContextRequest{
			UserID: event.User,
			View:   *view,
		},
	)
	if err != nil {
		h.logger.Error("Failed to publish Home Tab view",
			"user", event.User,
			"error", err)
		return fmt.Errorf("publish view: %w", err)
	}

	h.logger.Info("Published Home Tab view", "user", event.User)
	return nil
}

// HandleCapabilityClick handles a capability card button click.
// This opens the parameter Modal for the selected capability.
func (h *Handler) HandleCapabilityClick(ctx context.Context, callback *slack.InteractionCallback, capID string) error {
	if h.client == nil {
		return fmt.Errorf("slack client not configured")
	}

	h.logger.Debug("Handling capability click",
		"user", callback.User.ID,
		"capability", capID)

	// Get capability definition
	cap, ok := h.registry.Get(capID)
	if !ok {
		return fmt.Errorf("capability not found: %s", capID)
	}

	// Build Modal view
	modal := h.form.BuildModal(cap)

	// Open the Modal
	_, err := h.client.OpenViewContext(ctx, callback.TriggerID, *modal)
	if err != nil {
		h.logger.Error("Failed to open capability Modal",
			"user", callback.User.ID,
			"capability", capID,
			"error", err)
		return fmt.Errorf("open view: %w", err)
	}

	h.logger.Info("Opened capability Modal",
		"user", callback.User.ID,
		"capability", capID)
	return nil
}

// HandleViewSubmission handles Modal form submission.
// Returns ViewSubmissionResponse for validation errors, nil for success.
// The error return is for unexpected errors (not validation failures).
func (h *Handler) HandleViewSubmission(ctx context.Context, callback *slack.InteractionCallback) (*slack.ViewSubmissionResponse, error) {
	if h.client == nil {
		return nil, fmt.Errorf("slack client not configured")
	}

	capID := callback.View.PrivateMetadata
	userID := callback.User.ID

	h.logger.Debug("Handling view submission",
		"user", userID,
		"capability", capID)

	// Get capability definition
	cap, ok := h.registry.Get(capID)
	if !ok {
		return nil, fmt.Errorf("capability not found: %s", capID)
	}

	// Extract parameters
	params := h.form.ExtractParams(callback.View.State, cap)

	// Validate parameters
	if errors := h.form.ValidateParams(cap, params); len(errors) > 0 {
		h.logger.Warn("Parameter validation failed",
			"user", userID,
			"capability", capID,
			"errors", errors)
		// Return validation errors as Slack ViewSubmissionResponse
		return slack.NewErrorsViewSubmissionResponse(errors), nil
	}

	h.logger.Info("Executing capability",
		"user", userID,
		"capability", capID,
		"params", params)

	// Execute capability
	if h.executor != nil {
		go func() {
			// Execute in background to avoid blocking the response
			execCtx := context.Background()
			if err := h.executor.Execute(execCtx, userID, cap, params); err != nil {
				h.logger.Error("Capability execution failed",
					"user", userID,
					"capability", capID,
					"error", err)
			}
		}()
	} else {
		h.logger.Warn("No executor configured, capability will not be executed",
			"capability", capID)
	}

	// Return nil to close the Modal successfully
	return nil, nil
}

// IsCapabilityAction checks if an action ID is a capability click action.
func IsCapabilityAction(actionID string) bool {
	return strings.HasPrefix(actionID, ActionIDPrefix)
}

// ExtractCapabilityID extracts the capability ID from an action ID.
func ExtractCapabilityID(actionID string) string {
	return strings.TrimPrefix(actionID, ActionIDPrefix)
}

// SetClient sets the Slack client (for late initialization).
func (h *Handler) SetClient(client *slack.Client) {
	h.client = client
}

// SetExecutor sets the executor (for late initialization).
func (h *Handler) SetExecutor(executor *Executor) {
	h.executor = executor
}
