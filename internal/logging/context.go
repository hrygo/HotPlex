package logging

import "context"

// LogContext holds contextual information for logging.
// All fields are optional and used to enrich log entries with request/session context.
type LogContext struct {
	// SessionID is the unique identifier for the current session.
	SessionID string
	// ProviderSessionID is the session ID from the AI provider (e.g., Claude, OpenCode).
	ProviderSessionID string
	// Platform indicates which chat platform initiated the request (e.g., slack, telegram, ding).
	Platform string
	// Namespace is an optional grouping identifier for multi-tenant systems.
	Namespace string
	// UserID is the unique identifier of the user making the request.
	UserID string
	// ChannelID is the platform-specific channel identifier.
	ChannelID string
	// RequestID is a unique identifier for this specific request.
	RequestID string
	// Sensitivity indicates the sensitivity level of the content for masking purposes.
	Sensitivity SensitivityLevel // Reference to mask.go's SensitivityLevel
}

// NewLogContext creates a new LogContext with default values.
func NewLogContext() *LogContext {
	return &LogContext{
		Sensitivity: 0, // LevelNone = 0
	}
}

// WithSessionID returns a new LogContext with the given session ID.
func (c *LogContext) WithSessionID(sessionID string) *LogContext {
	c.SessionID = sessionID
	return c
}

// WithProviderSessionID returns a new LogContext with the given provider session ID.
func (c *LogContext) WithProviderSessionID(providerSessionID string) *LogContext {
	c.ProviderSessionID = providerSessionID
	return c
}

// WithPlatform returns a new LogContext with the given platform.
func (c *LogContext) WithPlatform(platform string) *LogContext {
	c.Platform = platform
	return c
}

// WithNamespace returns a new LogContext with the given namespace.
func (c *LogContext) WithNamespace(namespace string) *LogContext {
	c.Namespace = namespace
	return c
}

// WithUserID returns a new LogContext with the given user ID.
func (c *LogContext) WithUserID(userID string) *LogContext {
	c.UserID = userID
	return c
}

// WithChannelID returns a new LogContext with the given channel ID.
func (c *LogContext) WithChannelID(channelID string) *LogContext {
	c.ChannelID = channelID
	return c
}

// WithRequestID returns a new LogContext with the given request ID.
func (c *LogContext) WithRequestID(requestID string) *LogContext {
	c.RequestID = requestID
	return c
}

// WithSensitivity returns a new LogContext with the given sensitivity level.
func (c *LogContext) WithSensitivity(level SensitivityLevel) *LogContext {
	c.Sensitivity = level
	return c
}

// toAttrs converts LogContext to slog attributes.
func (c *LogContext) toAttrs() []any {
	attrs := make([]any, 0, 8)

	if c.SessionID != "" {
		attrs = append(attrs, FieldSessionID, c.SessionID)
	}
	if c.ProviderSessionID != "" {
		attrs = append(attrs, FieldProviderSessionID, c.ProviderSessionID)
	}
	if c.Platform != "" {
		attrs = append(attrs, FieldPlatform, c.Platform)
	}
	if c.Namespace != "" {
		attrs = append(attrs, FieldNamespace, c.Namespace)
	}
	if c.UserID != "" {
		attrs = append(attrs, FieldUserID, c.UserID)
	}
	if c.ChannelID != "" {
		attrs = append(attrs, FieldChannelID, c.ChannelID)
	}
	if c.RequestID != "" {
		attrs = append(attrs, FieldRequestID, c.RequestID)
	}

	return attrs
}

// contextKey is used for storing LogContext in context.Context.
type contextKey struct{}

// contextKey is the key for storing LogContext in context.Context.
var logContextKey = contextKey{}

// WithContext returns a new context with the given LogContext stored.
func (c *LogContext) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, logContextKey, c)
}

// FromContext retrieves a LogContext from the context, if present.
func FromContext(ctx context.Context) (*LogContext, bool) {
	c, ok := ctx.Value(logContextKey).(*LogContext)
	return c, ok
}
