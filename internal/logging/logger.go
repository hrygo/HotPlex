package logging

import (
	"context"
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
func NewLogger(logger *slog.Logger, opts ...Option) *Logger {
	if logger == nil {
		logger = slog.Default()
	}

	l := &Logger{
		logger:      logger,
		ctx:         NewLogContext(),
		sensitivity: LevelNone,
		floatFormat: FloatPrecise,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// With returns a new Logger with the given context merged.
func (l *Logger) With(ctx *LogContext) *Logger {
	if ctx == nil {
		return l
	}

	newLogger := &Logger{
		logger:      l.logger,
		ctx:         ctx,
		sensitivity: l.sensitivity,
		floatFormat: l.floatFormat,
	}

	// If context has sensitivity set, use it
	if ctx.Sensitivity != LevelNone {
		newLogger.sensitivity = ctx.Sensitivity
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
func (l *Logger) maskAttrs(attrs []any) []any {
	result := make([]any, len(attrs))
	for i, arg := range attrs {
		if i%2 == 1 { // This is a value
			if str, ok := arg.(string); ok {
				result[i] = MaskString(str, l.sensitivity)
			} else {
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

// WithAttrs returns a new Logger with additional attributes.
func (l *Logger) WithAttrs(attrs ...any) *slog.Logger {
	return l.logger.With(attrs...)
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
func (l *Logger) LogError(err error, msg string, args ...any) {
	attrs := l.buildAttrs(append([]any{FieldError, err.Error()}, args...)...)
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

// SetLevel changes the logger's level.
func (l *Logger) SetLevel(level slog.Level) {
	_ = l.logger.Handler().Enabled(context.Background(), level)
}

// GetLogger returns the underlying slog.Logger.
func (l *Logger) GetLogger() *slog.Logger {
	return l.logger
}
