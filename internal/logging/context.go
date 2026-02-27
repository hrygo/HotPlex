package logging

import (
	"context"
	"strings"
)

// MaxFieldLength is the maximum length for string fields in LogContext.
// Fields longer than this will be truncated in log output.
const MaxFieldLength = 256

// LogContext holds contextual information for logging.
// All fields are optional and used to enrich log entries with request/session context.
// Field values are automatically sanitized: control characters removed and truncated to MaxFieldLength.
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

// sanitizeField cleans a field value for safe logging:
// - Removes control characters (newlines, tabs, etc.) to prevent log injection
// - Truncates to MaxFieldLength to prevent log bloating
func sanitizeField(s string) string {
	// Remove control characters to prevent log injection
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 32 || r == '\t' { // Allow printable chars and tab
			b.WriteRune(r)
		}
	}
	result := b.String()

	// Truncate if too long
	if len(result) > MaxFieldLength {
		return result[:MaxFieldLength-3] + "..."
	}
	return result
}

// NewLogContext creates a new LogContext with default values.
func NewLogContext() *LogContext {
	return &LogContext{
		Sensitivity: 0, // LevelNone = 0
	}
}

// WithSessionID returns a new LogContext with the given session ID.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithSessionID(sessionID string) *LogContext {
	newCtx := *c
	newCtx.SessionID = sessionID
	return &newCtx
}

// WithProviderSessionID returns a new LogContext with the given provider session ID.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithProviderSessionID(providerSessionID string) *LogContext {
	newCtx := *c
	newCtx.ProviderSessionID = providerSessionID
	return &newCtx
}

// WithPlatform returns a new LogContext with the given platform.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithPlatform(platform string) *LogContext {
	newCtx := *c
	newCtx.Platform = platform
	return &newCtx
}

// WithNamespace returns a new LogContext with the given namespace.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithNamespace(namespace string) *LogContext {
	newCtx := *c
	newCtx.Namespace = namespace
	return &newCtx
}

// WithUserID returns a new LogContext with the given user ID.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithUserID(userID string) *LogContext {
	newCtx := *c
	newCtx.UserID = userID
	return &newCtx
}

// WithChannelID returns a new LogContext with the given channel ID.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithChannelID(channelID string) *LogContext {
	newCtx := *c
	newCtx.ChannelID = channelID
	return &newCtx
}

// WithRequestID returns a new LogContext with the given request ID.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithRequestID(requestID string) *LogContext {
	newCtx := *c
	newCtx.RequestID = requestID
	return &newCtx
}

// WithSensitivity returns a new LogContext with the given sensitivity level.
// Returns a copy to maintain immutability for concurrent safety.
func (c *LogContext) WithSensitivity(level SensitivityLevel) *LogContext {
	newCtx := *c
	newCtx.Sensitivity = level
	return &newCtx
}

// toAttrs converts LogContext to slog attributes.
// All string values are sanitized to prevent log injection and truncation.
func (c *LogContext) toAttrs() []any {
	attrs := make([]any, 0, 8)

	if c.SessionID != "" {
		attrs = append(attrs, FieldSessionID, sanitizeField(c.SessionID))
	}
	if c.ProviderSessionID != "" {
		attrs = append(attrs, FieldProviderSessionID, sanitizeField(c.ProviderSessionID))
	}
	if c.Platform != "" {
		attrs = append(attrs, FieldPlatform, sanitizeField(c.Platform))
	}
	if c.Namespace != "" {
		attrs = append(attrs, FieldNamespace, sanitizeField(c.Namespace))
	}
	if c.UserID != "" {
		attrs = append(attrs, FieldUserID, sanitizeField(c.UserID))
	}
	if c.ChannelID != "" {
		attrs = append(attrs, FieldChannelID, sanitizeField(c.ChannelID))
	}
	if c.RequestID != "" {
		attrs = append(attrs, FieldRequestID, sanitizeField(c.RequestID))
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
