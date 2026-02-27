package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewLogContext(t *testing.T) {
	ctx := NewLogContext()
	if ctx.Sensitivity != LevelNone {
		t.Errorf("expected default sensitivity LevelNone, got %v", ctx.Sensitivity)
	}
}

func TestLogContext_WithMethods(t *testing.T) {
	ctx := NewLogContext().
		WithSessionID("sess-123").
		WithProviderSessionID("prov-456").
		WithPlatform("slack").
		WithNamespace("production").
		WithUserID("user-789").
		WithChannelID("channel-abc").
		WithRequestID("req-xyz").
		WithSensitivity(LevelMedium)

	tests := []struct {
		got  string
		want string
		name string
	}{
		{ctx.SessionID, "sess-123", "SessionID"},
		{ctx.ProviderSessionID, "prov-456", "ProviderSessionID"},
		{ctx.Platform, "slack", "Platform"},
		{ctx.Namespace, "production", "Namespace"},
		{ctx.UserID, "user-789", "UserID"},
		{ctx.ChannelID, "channel-abc", "ChannelID"},
		{ctx.RequestID, "req-xyz", "RequestID"},
		{ctx.Sensitivity.String(), "medium", "Sensitivity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestLogContext_toAttrs(t *testing.T) {
	ctx := &LogContext{
		SessionID:         "sess-123",
		ProviderSessionID: "prov-456",
		Platform:          "slack",
		UserID:            "user-789",
	}

	attrs := ctx.toAttrs()
	// 4 fields = 8 elements (key-value pairs)
	if len(attrs) != 8 {
		t.Errorf("expected 8 attributes, got %d", len(attrs))
	}
}

func TestMaskString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		level    SensitivityLevel
		expected string
	}{
		// LevelNone - no masking
		{"LevelNone", "secret123", LevelNone, "secret123"},
		{"LevelNone empty", "", LevelNone, ""},
		// LevelLow - short strings now masked for security
		{"LevelLow short 3", "abc", LevelLow, "a****"},
		{"LevelLow short 5", "abcde", LevelLow, "ab****de"},
		{"LevelLow 8 chars", "sk-abcde", LevelLow, "sk****de"},
		{"LevelLow 10 chars", "sk-abcd123", LevelLow, "sk-a****d123"},
		{"LevelLow normal", "sk-abc123xyz789", LevelLow, "sk-a****z789"},
		// LevelMedium - short strings now masked for security
		{"LevelMedium short 4", "abcd", LevelMedium, "a****"},
		{"LevelMedium 5 chars", "U0K2x", LevelMedium, "U0****2x"},
		{"LevelMedium normal", "U0ClK2x", LevelMedium, "U0****2x"},
		// LevelHigh - always mask, showing only first char
		{"LevelHigh empty", "", LevelHigh, ""},
		{"LevelHigh single", "a", LevelHigh, "a****"},
		{"LevelHigh normal", "secret123", LevelHigh, "s****"},
		// Unicode support
		{"LevelHigh unicode", "世界", LevelHigh, "世****"},
		{"LevelLow unicode", "你好世界测试", LevelLow, "你好****测试"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskString(tt.input, tt.level)
			if result != tt.expected {
				t.Errorf("MaskString(%q, %v) = %q, want %q", tt.input, tt.level, result, tt.expected)
			}
		})
	}
}

func TestSensitivityLevel_String(t *testing.T) {
	tests := []struct {
		level    SensitivityLevel
		expected string
	}{
		{LevelNone, "none"},
		{LevelLow, "low"},
		{LevelMedium, "medium"},
		{LevelHigh, "high"},
		{SensitivityLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskStrings(t *testing.T) {
	input := map[string]string{
		"api_key":  "sk-abc123xyz789",
		"username": "john_doe", // 8 chars - now masked
		"password": "super_secret",
	}

	result := MaskStrings(input, LevelLow)

	if result["api_key"] != "sk-a****z789" {
		t.Errorf("expected sk-a****z789, got %s", result["api_key"])
	}
	// john_doe (8 chars) is now masked at LevelLow: jo****oe
	if result["username"] != "jo****oe" {
		t.Errorf("expected jo****oe, got %s", result["username"])
	}
	if result["password"] != "supe****cret" {
		t.Errorf("expected supe****cret, got %s", result["password"])
	}
}

func TestMaskSensitiveFields(t *testing.T) {
	input := map[string]any{
		"api_key":  "sk-secret123456",
		"username": "john",
		"token":    "tok_abc123",
		"data":     "some data",
	}

	result := MaskSensitiveFields(input, LevelMedium)

	if result["api_key"] != "sk****56" {
		t.Errorf("expected sk****56, got %v", result["api_key"])
	}
	if result["username"] != "john" {
		t.Errorf("expected john, got %v", result["username"])
	}
	if result["token"] != "to****23" {
		t.Errorf("expected to****23, got %v", result["token"])
	}
	if result["data"] != "some data" {
		t.Errorf("expected some data, got %v", result["data"])
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		input    float64
		format   FloatFormat
		expected float64
	}{
		{1.234, FloatPrecise, 1.23},
		{1.235, FloatPrecise, 1.24},
		{1.999, FloatPrecise, 2.0},
		{1.234, FloatRaw, 1.234},
		{1.999999, FloatRaw, 1.999999},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := FormatFloat(tt.input, tt.format)
			if result != tt.expected {
				t.Errorf("FormatFloat(%v, %v) = %v, want %v", tt.input, tt.format, result, tt.expected)
			}
		})
	}
}

func TestFloatValue(t *testing.T) {
	fv := NewFloatValue(1.234567, FloatPrecise)
	if fv.Value != 1.23 {
		t.Errorf("expected 1.23, got %v", fv.Value)
	}
	if fv.String() != "1.23" {
		t.Errorf("expected 1.23, got %s", fv.String())
	}

	fvRaw := NewFloatValue(1.234567, FloatRaw)
	if fvRaw.String() != "1.234567" {
		t.Errorf("expected 1.234567, got %s", fvRaw.String())
	}
}

func TestDurationMs(t *testing.T) {
	dm := DurationMs(1500)
	if dm.ToDuration() != 1500*time.Millisecond {
		t.Errorf("expected 1500ms, got %v", dm.ToDuration())
	}
}

// newTestLogger creates a Logger with a buffer for capturing output
func newTestLogger(buf *bytes.Buffer) *Logger {
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	baseLogger := slog.New(handler)
	return NewLogger(baseLogger)
}

func TestNewLogger(t *testing.T) {
	t.Run("with nil logger uses default", func(t *testing.T) {
		l := NewLogger(nil)
		if l == nil {
			t.Error("expected non-nil logger")
		}
	})

	t.Run("default sensitivity is LevelLow", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)
		if l.sensitivity != LevelLow {
			t.Errorf("expected default sensitivity LevelLow, got %v", l.sensitivity)
		}
	})

	t.Run("with options", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)
		l2 := NewLogger(l.GetLogger(), WithSensitivity(LevelHigh), WithFloatFormat(FloatRaw))
		if l2.sensitivity != LevelHigh {
			t.Errorf("expected LevelHigh, got %v", l2.sensitivity)
		}
		if l2.floatFormat != FloatRaw {
			t.Errorf("expected FloatRaw, got %v", l2.floatFormat)
		}
	})
}

func TestLogger_With(t *testing.T) {
	t.Run("nil context returns self", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)
		l2 := l.With(nil)
		if l != l2 {
			t.Error("expected same logger instance")
		}
	})

	t.Run("merges context fields", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)

		ctx1 := NewLogContext().WithSessionID("session-1").WithPlatform("slack")
		l2 := l.With(ctx1)

		ctx2 := NewLogContext().WithUserID("user-2").WithChannelID("channel-2")
		l3 := l2.With(ctx2)

		// Should have all fields merged
		if l3.ctx.SessionID != "session-1" {
			t.Errorf("expected session-1, got %s", l3.ctx.SessionID)
		}
		if l3.ctx.Platform != "slack" {
			t.Errorf("expected slack, got %s", l3.ctx.Platform)
		}
		if l3.ctx.UserID != "user-2" {
			t.Errorf("expected user-2, got %s", l3.ctx.UserID)
		}
		if l3.ctx.ChannelID != "channel-2" {
			t.Errorf("expected channel-2, got %s", l3.ctx.ChannelID)
		}
	})

	t.Run("uses higher sensitivity", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)

		ctx := NewLogContext().WithSensitivity(LevelHigh)
		l2 := l.With(ctx)

		if l2.sensitivity != LevelHigh {
			t.Errorf("expected LevelHigh, got %v", l2.sensitivity)
		}
	})
}

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)

	l.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected output to contain 'test message', got %s", output)
	}
	if !strings.Contains(output, "key") {
		t.Errorf("expected output to contain 'key', got %s", output)
	}
}

func TestLogger_LogError(t *testing.T) {
	t.Run("nil error returns early", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)

		l.LogError(nil, "test error")
		if buf.Len() > 0 {
			t.Error("expected no output for nil error")
		}
	})

	t.Run("logs error message", func(t *testing.T) {
		var buf bytes.Buffer
		l := newTestLogger(&buf)

		testErr := context.Canceled
		l.LogError(testErr, "operation failed")

		output := buf.String()
		if !strings.Contains(output, "operation failed") {
			t.Errorf("expected output to contain 'operation failed', got %s", output)
		}
	})
}

func TestLogger_Named(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)

	l2 := l.Named("mylogger")
	l2.Info("test")

	output := buf.String()
	if !strings.Contains(output, "mylogger") {
		t.Errorf("expected output to contain 'mylogger', got %s", output)
	}
}

func TestLogger_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)

	l2 := l.WithAttrs("extra", "attr")
	l2.Info("test")

	output := buf.String()
	if !strings.Contains(output, "extra") {
		t.Errorf("expected output to contain 'extra', got %s", output)
	}
}

func TestLogger_LogTiming(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)

	start := time.Now().Add(-100 * time.Millisecond)
	l.LogTiming("operation", start)

	output := buf.String()
	if !strings.Contains(output, "timing") {
		t.Errorf("expected output to contain 'timing', got %s", output)
	}
	if !strings.Contains(output, "operation") {
		t.Errorf("expected output to contain 'operation', got %s", output)
	}
}

func TestSanitizeField(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal string", "hello", "hello"},
		{"removes newline", "hello\nworld", "helloworld"},
		{"removes carriage return", "hello\r\nworld", "helloworld"},
		{"removes null byte", "hello\x00world", "helloworld"},
		{"keeps tab", "hello\tworld", "hello\tworld"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeField(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeField(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}

	t.Run("truncates long string", func(t *testing.T) {
		longStr := strings.Repeat("a", 300)
		result := sanitizeField(longStr)
		if len(result) > MaxFieldLength {
			t.Errorf("expected length <= %d, got %d", MaxFieldLength, len(result))
		}
		if !strings.HasSuffix(result, "...") {
			t.Errorf("expected truncated string to end with '...', got %q", result)
		}
	})
}

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		field    string
		expected bool
	}{
		{"api_key", true},
		{"apiKey", true},
		{"API_KEY", true},
		{"api-key", true},
		{"password", true},
		{"PASSWORD", true},
		{"token", true},
		{"access_token", true},
		{"authToken", true},
		{"secret", true},
		{"client_secret", true},
		{"mySecretKey", true},
		{"username", false},
		{"email", false},
		{"data", false},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			result := isSensitiveField(tt.field)
			if result != tt.expected {
				t.Errorf("isSensitiveField(%q) = %v, want %v", tt.field, result, tt.expected)
			}
		})
	}
}
