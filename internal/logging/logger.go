package logging

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Logger is a structured logger with context and sensitivity support.
// It wraps slog.Logger and provides methods for consistent logging.
type Logger struct {
	logger      *slog.Logger
	ctx         *LogContext
	sensitivity SensitivityLevel
	floatFormat FloatFormat
}

// Interface defines the logging interface.
// This allows for interface compliance verification.
type Interface interface {
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(ctx *LogContext) *Logger
}

// Compile-time interface compliance check.
var _ Interface = (*Logger)(nil)

// Option is a functional option for configuring a Logger.
type Option func(*Logger)

// WithSensitivity sets the sensitivity level for the logger.
func WithSensitivity(level SensitivityLevel) Option {
	return func(l *Logger) {
		l.sensitivity = level
	}
}

// WithFloatFormat sets the float format for the logger.
func WithFloatFormat(format FloatFormat) Option {
	return func(l *Logger) {
		l.floatFormat = format
	}
}

// NewLogger creates a new Logger with the given options.
// If no logger is provided, it uses the default slog logger.
// Default sensitivity is LevelLow for secure-by-default behavior.
func NewLogger(logger *slog.Logger, opts ...Option) *Logger {
	if logger == nil {
		logger = slog.Default()
	}

	l := &Logger{
		logger:      logger,
		ctx:         NewLogContext(),
		sensitivity: LevelLow, // Secure-by-default: enable basic masking
		floatFormat: FloatPrecise,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// With returns a new Logger with the given context merged into the existing context.
// Non-empty fields in the provided context override existing values.
// Sensitivity level is set to the higher of the two levels.
func (l *Logger) With(ctx *LogContext) *Logger {
	if ctx == nil {
		return l
	}

	// Merge contexts: start with existing, override with non-empty new values
	merged := &LogContext{}
	if l.ctx != nil {
		*merged = *l.ctx
	}

	// Override with non-empty fields from new context
	if ctx.SessionID != "" {
		merged.SessionID = ctx.SessionID
	}
	if ctx.ProviderSessionID != "" {
		merged.ProviderSessionID = ctx.ProviderSessionID
	}
	if ctx.Platform != "" {
		merged.Platform = ctx.Platform
	}
	if ctx.Namespace != "" {
		merged.Namespace = ctx.Namespace
	}
	if ctx.UserID != "" {
		merged.UserID = ctx.UserID
	}
	if ctx.ChannelID != "" {
		merged.ChannelID = ctx.ChannelID
	}
	if ctx.RequestID != "" {
		merged.RequestID = ctx.RequestID
	}
	// Use higher sensitivity level
	if ctx.Sensitivity > merged.Sensitivity {
		merged.Sensitivity = ctx.Sensitivity
	}

	newLogger := &Logger{
		logger:      l.logger,
		ctx:         merged,
		sensitivity: l.sensitivity,
		floatFormat: l.floatFormat,
	}

	// Update sensitivity if merged context has higher level
	if merged.Sensitivity > newLogger.sensitivity {
		newLogger.sensitivity = merged.Sensitivity
	}

	return newLogger
}

// buildAttrs builds the attribute list for logging.
func (l *Logger) buildAttrs(args ...any) []any {
	attrs := make([]any, 0, len(args)+8)

	// Add context attributes first
	if l.ctx != nil {
		attrs = append(attrs, l.ctx.toAttrs()...)
	}

	// Add user-provided attributes
	attrs = append(attrs, args...)

	// Apply sensitivity masking to string values
	if l.sensitivity != LevelNone {
		attrs = l.maskAttrs(attrs)
	}

	return attrs
}

// maskAttrs applies sensitivity masking to string values in attributes.
// Handles string, []byte, and fmt.Stringer types to prevent bypass of masking.
func (l *Logger) maskAttrs(attrs []any) []any {
	result := make([]any, len(attrs))
	for i, arg := range attrs {
		if i%2 == 1 { // This is a value
			switch v := arg.(type) {
			case string:
				result[i] = MaskString(v, l.sensitivity)
			case []byte:
				// Convert []byte to string, mask, then convert back
				result[i] = []byte(MaskString(string(v), l.sensitivity))
			case fmt.Stringer:
				// Handle types that implement String() (e.g., custom types)
				result[i] = MaskString(v.String(), l.sensitivity)
			default:
				result[i] = arg
			}
		} else {
			result[i] = arg
		}
	}
	return result
}

// Info logs a message at INFO level.
func (l *Logger) Info(msg string, args ...any) {
	attrs := l.buildAttrs(args...)
	l.logger.Info(msg, attrs...)
}

// Debug logs a message at DEBUG level.
func (l *Logger) Debug(msg string, args ...any) {
	attrs := l.buildAttrs(args...)
	l.logger.Debug(msg, attrs...)
}

// Warn logs a message at WARN level.
func (l *Logger) Warn(msg string, args ...any) {
	attrs := l.buildAttrs(args...)
	l.logger.Warn(msg, attrs...)
}

// Error logs a message at ERROR level.
func (l *Logger) Error(msg string, args ...any) {
	attrs := l.buildAttrs(args...)
	l.logger.Error(msg, attrs...)
}

// LogAttrs logs a message with pre-built attributes.
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(ctx, level, msg, attrs...)
}

// WithAttrs returns a new Logger with additional attributes added to the underlying slog.Logger.
// This maintains the Logger wrapper pattern for consistent API usage.
func (l *Logger) WithAttrs(attrs ...any) *Logger {
	newLogger := *l
	newLogger.logger = l.logger.With(attrs...)
	return &newLogger
}

// Named returns a new Logger with the given name.
func (l *Logger) Named(name string) *Logger {
	newLogger := *l
	newLogger.logger = l.logger.With(slog.String("logger", name))
	return &newLogger
}

// LogTiming logs timing information with proper duration formatting.
func (l *Logger) LogTiming(operation string, start time.Time, args ...any) {
	duration := time.Since(start)
	attrs := l.buildAttrs(append([]any{FieldOperation, operation, FieldDurationMs, DurationMs(duration.Milliseconds())}, args...)...)
	l.logger.Info("timing", attrs...)
}

// LogError logs an error with optional context.
// The error message is masked if sensitivity level is set to prevent sensitive data leakage.
func (l *Logger) LogError(err error, msg string, args ...any) {
	if err == nil {
		return
	}
	// Build args with error message that will be masked if sensitivity is enabled
	errArgs := append([]any{FieldError, err.Error()}, args...)
	attrs := l.buildAttrs(errArgs...)
	l.logger.Error(msg, attrs...)
}

// LogRequest logs a request with standard fields.
func (l *Logger) LogRequest(operation string, inputLen int, outputLen int, args ...any) {
	attrs := l.buildAttrs(append([]any{
		FieldOperation, operation,
		FieldInputLen, inputLen,
		FieldOutputLen, outputLen,
	}, args...)...)
	l.logger.Info("request", attrs...)
}

// GetLogger returns the underlying slog.Logger.
func (l *Logger) GetLogger() *slog.Logger {
	return l.logger
}
