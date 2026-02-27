package logging

import (
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
		{"LevelNone", "secret123", LevelNone, "secret123"},
		{"LevelLow short", "abc", LevelLow, "abc"},
		{"LevelLow 8 chars", "sk-abcde", LevelLow, "sk-abcde"},
		{"LevelLow 10 chars", "sk-abcd123", LevelLow, "sk-a****d123"},
		{"LevelLow normal", "sk-abc123xyz789", LevelLow, "sk-a****z789"},
		{"LevelMedium short", "abcd", LevelMedium, "abcd"},
		{"LevelMedium 5 chars", "U0K2x", LevelMedium, "U0****2x"},
		{"LevelMedium normal", "U0ClK2x", LevelMedium, "U0****2x"},
		{"LevelHigh empty", "", LevelHigh, ""},
		{"LevelHigh single", "a", LevelHigh, "a****"},
		{"LevelHigh normal", "secret123", LevelHigh, "s****"},
		{"LevelNone empty", "", LevelNone, ""},
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
		"username": "john_doe",
		"password": "super_secret",
	}

	result := MaskStrings(input, LevelLow)

	if result["api_key"] != "sk-a****z789" {
		t.Errorf("expected sk-a****z789, got %s", result["api_key"])
	}
	if result["username"] != "john_doe" {
		t.Errorf("expected john_doe, got %s", result["username"])
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
