package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// ========================================
// Mock LLM Client for Brain Init Tests
// ========================================

// mockLLMClientForBrain implements llm.LLMClient interface for testing.
type mockLLMClientForBrain struct {
	chatFn    func(ctx context.Context, prompt string) (string, error)
	chatCount int
}

func (m *mockLLMClientForBrain) Chat(ctx context.Context, prompt string) (string, error) {
	m.chatCount++
	if m.chatFn != nil {
		return m.chatFn(ctx, prompt)
	}
	return "mock chat response", nil
}

func (m *mockLLMClientForBrain) ChatWithOptions(ctx context.Context, prompt string, opts llm.ChatOptions) (string, error) {
	return m.Chat(ctx, prompt)
}

func (m *mockLLMClientForBrain) Analyze(ctx context.Context, prompt string, target any) error {
	return json.Unmarshal([]byte(`{"result": "mock"}`), target)
}

func (m *mockLLMClientForBrain) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *mockLLMClientForBrain) HealthCheck(ctx context.Context) llm.HealthStatus {
	return llm.HealthStatus{Healthy: true}
}

// ========================================
// enhancedBrainWrapper Tests
// ========================================

func TestEnhancedBrainWrapper_Chat(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	result, err := wrapper.Chat(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "mock chat response", result)
	assert.Equal(t, 1, mockClient.chatCount)
}

func TestEnhancedBrainWrapper_ApplyTimeout_WithConfig(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config:  Config{Model: ModelConfig{TimeoutS: 10}},
		timeout: 10 * time.Second, // Pre-computed timeout
	}

	ctx := context.Background()
	modifiedCtx, cancel := wrapper.applyTimeout(ctx)
	defer cancel()

	// The context should have a deadline
	deadline, ok := modifiedCtx.Deadline()
	assert.True(t, ok, "context should have a deadline")
	assert.WithinDuration(t, time.Now().Add(10*time.Second), deadline, time.Second)
}

func TestEnhancedBrainWrapper_ApplyTimeout_NoConfig(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{TimeoutS: 0}},
	}

	ctx := context.Background()
	modifiedCtx, cancel := wrapper.applyTimeout(ctx)
	defer cancel()

	// The context should NOT have a deadline
	_, ok := modifiedCtx.Deadline()
	assert.False(t, ok)
}

// ========================================
// selectModel Tests
// ========================================

func TestEnhancedBrainWrapper_SelectModel_ExplicitModel(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	model := wrapper.selectModel(context.Background(), "gpt-4o-mini", "chat")
	assert.Equal(t, "gpt-4o-mini", model)
}

func TestEnhancedBrainWrapper_SelectModel_NoRouter(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		router: nil,
	}

	model := wrapper.selectModel(context.Background(), "", "chat")
	assert.Equal(t, "gpt-4o", model)
}

// ========================================
// applyRateLimit Tests
// ========================================

func TestEnhancedBrainWrapper_ApplyRateLimit_NoLimiter(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		rateLimiter: nil,
	}

	err := wrapper.applyRateLimit(context.Background(), "gpt-4o")
	assert.NoError(t, err)
}

// ========================================
// startMetricsTimer Tests
// ========================================

func TestEnhancedBrainWrapper_StartMetricsTimer_NilMetrics(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		metrics: nil,
	}

	timer := wrapper.startMetricsTimer("gpt-4o", "chat")
	assert.Nil(t, timer)
}

// ========================================
// recordMetrics Tests
// ========================================

func TestEnhancedBrainWrapper_RecordMetrics_NilTimer(t *testing.T) {
	wrapper := &enhancedBrainWrapper{}
	// Should not panic
	wrapper.recordMetrics(nil, "gpt-4o", "prompt", "result", nil)
}

func TestEnhancedBrainWrapper_RecordMetrics_NoCostCalc(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{},
		// metrics is nil, so timer will be nil
	}
	// Should not panic
	wrapper.recordMetrics(nil, "gpt-4o", "prompt", "result", nil)
}

// ========================================
// recordMetricsForAnalyze Tests
// ========================================

func TestInit_Disabled(t *testing.T) {
	// Save and restore global state
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	// Clear global brain to simulate fresh start
	globalBrain = nil

	// Clear all env vars that could enable the brain
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")

	err := Init(slog.Default())
	assert.NoError(t, err)
	assert.Nil(t, Global())
}

// ========================================
// Interface Compliance
// ========================================

func TestGetBoolEnv(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback bool
		expected bool
	}{
		{"true string", "true", false, true},
		{"false string", "false", true, false},
		{"invalid", "notbool", false, false},
		{"invalid with true fallback", "notbool", true, true},
		{"empty env", "", true, true},
		{"empty env false fallback", "", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				t.Setenv("TEST_BOOL_ENV", tc.value)
			}
			result := getBoolEnv("TEST_BOOL_ENV", tc.fallback)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ========================================
// getIntEnv Tests
// ========================================

func TestGetIntEnv(t *testing.T) {
	t.Setenv("TEST_INT_ENV", "42")
	result := getIntEnv("TEST_INT_ENV", 0)
	assert.Equal(t, 42, result)

	result = getIntEnv("TEST_INT_NONEXISTENT", 99)
	assert.Equal(t, 99, result)

	t.Setenv("TEST_INT_INVALID", "notanumber")
	result = getIntEnv("TEST_INT_INVALID", 77)
	assert.Equal(t, 77, result)
}

// ========================================
// getFloatEnv Tests
// ========================================

func TestGetFloatEnv(t *testing.T) {
	t.Setenv("TEST_FLOAT_ENV", "3.14")
	result := getFloatEnv("TEST_FLOAT_ENV", 0.0)
	assert.InDelta(t, 3.14, result, 0.001)

	result = getFloatEnv("TEST_FLOAT_NONEXISTENT", 1.5)
	assert.InDelta(t, 1.5, result, 0.001)
}

// ========================================
// getDurationEnv Tests
// ========================================

func TestGetDurationEnv_DurationString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "30s")
	result := getDurationEnv("TEST_DUR_ENV", 5*time.Second)
	assert.Equal(t, 30*time.Second, result)
}

func TestGetDurationEnv_SecondsString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "60")
	result := getDurationEnv("TEST_DUR_ENV", 5*time.Second)
	assert.Equal(t, 60*time.Second, result)
}

func TestGetDurationEnv_Fallback(t *testing.T) {
	result := getDurationEnv("TEST_DUR_NONEXISTENT", 10*time.Second)
	assert.Equal(t, 10*time.Second, result)
}

func TestGetDurationEnv_InvalidString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "notaduration")
	result := getDurationEnv("TEST_DUR_ENV", 10*time.Second)
	assert.Equal(t, 10*time.Second, result)
}

// ========================================
// parseRouterModels Tests
// ========================================

func TestParseRouterModels_Empty(t *testing.T) {
	result := parseRouterModels("")
	assert.Nil(t, result)
}

func TestParseRouterModels_SingleModel(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)
	assert.Equal(t, "openai", result[0].Provider)
	assert.InDelta(t, 0.03, result[0].CostPer1KInput, 0.001)
	assert.InDelta(t, 0.06, result[0].CostPer1KOutput, 0.001)
	assert.Equal(t, int64(500), result[0].AvgLatencyMs)
	assert.True(t, result[0].Enabled)
}

func TestParseRouterModels_MultipleModels(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500;claude-3:anthropic:0.015:0.075:800")
	require.Len(t, result, 2)
	assert.Equal(t, "gpt-4o", result[0].Name)
	assert.Equal(t, "claude-3", result[1].Name)
}

func TestParseRouterModels_TooFewFields(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai")
	assert.Nil(t, result)
}

func TestParseRouterModels_WhitespaceHandling(t *testing.T) {
	// parseRouterModels trims whitespace around parts but not within fields
	// So "gpt-4o " as a model name will be preserved
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)

	// Test with outer whitespace (trimmed by strings.TrimSpace on parts)
	result = parseRouterModels(" gpt-4o:openai:0.03:0.06:500 ")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)
}

// ========================================
// parseStringList Tests
// ========================================

func TestParseStringList_Empty(t *testing.T) {
	result := parseStringList("")
	assert.Nil(t, result)
}

func TestParseStringList_Single(t *testing.T) {
	result := parseStringList("admin-user")
	require.Len(t, result, 1)
	assert.Equal(t, "admin-user", result[0])
}

func TestParseStringList_Multiple(t *testing.T) {
	result := parseStringList("user1,user2,user3")
	require.Len(t, result, 3)
	assert.Equal(t, []string{"user1", "user2", "user3"}, result)
}

func TestParseStringList_WithWhitespace(t *testing.T) {
	result := parseStringList(" user1 , user2 , user3 ")
	require.Len(t, result, 3)
	assert.Equal(t, []string{"user1", "user2", "user3"}, result)
}

func TestParseStringList_EmptyElements(t *testing.T) {
	result := parseStringList("user1,,user2,")
	require.Len(t, result, 2)
	assert.Equal(t, []string{"user1", "user2"}, result)
}

// ========================================
// LoadConfigFromEnv with OpenAI base URL
// ========================================

func TestConfig_OpenAIEndpointFallback(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "oa-test-key")
	t.Setenv("OPENAI_BASE_URL", "https://custom.openai.com/v1")

	config, _ := LoadConfigFromEnv()

	assert.Equal(t, "https://custom.openai.com/v1", config.Model.Endpoint)
}

// ========================================
// Env var helpers with t.Setenv
// ========================================

func TestGetEnv_Empty(t *testing.T) {
	t.Setenv("TEST_GETENV_EMPTY", "")
	result := getEnv("TEST_GETENV_EMPTY", "fallback")
	assert.Equal(t, "fallback", result)
}

func TestGetIntEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_INTVAR", "256")
	result := getIntEnv("TEST_INTVAR", 0)
	assert.Equal(t, 256, result)
}

func TestGetBoolEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_BOOLVAR", "true")
	result := getBoolEnv("TEST_BOOLVAR", false)
	assert.True(t, result)
}

func TestGetFloatEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_FLOATVAR", "99.9")
	result := getFloatEnv("TEST_FLOATVAR", 0.0)
	assert.InDelta(t, 99.9, result, 0.01)
}

func TestGetDurationEnv_WithDurationString(t *testing.T) {
	t.Setenv("TEST_DURVAR", "5m")
	result := getDurationEnv("TEST_DURVAR", 0)
	assert.Equal(t, 5*time.Minute, result)
}

// ========================================
// Strings helpers used in guard
// ========================================

func TestTruncateForAnalysis_ExactBoundary(t *testing.T) {
	s := strings.Repeat("x", 500)
	result := truncate(s)
	assert.Equal(t, s, result)
	assert.NotContains(t, result, "...")
}

func TestTruncateForAnalysis_OneOver(t *testing.T) {
	s := strings.Repeat("x", 501)
	result := truncate(s)
	assert.Len(t, result, 500) // truncate ensures max 500 chars
	assert.True(t, strings.HasSuffix(result, "..."))
}

func TestTruncateForAnalysis_Empty(t *testing.T) {
	result := truncate("")
	assert.Equal(t, "", result)
}

// ========================================
// Init brain config override tests
// ========================================

func TestConfig_MetricsServiceName(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_METRICS_SERVICE_NAME", "custom-service")

	config, _ := LoadConfigFromEnv()
	assert.Equal(t, "custom-service", config.Metrics.ServiceName)
}

func TestConfig_RouterStrategy(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_ROUTER_STRATEGY", "latency_priority")

	config, _ := LoadConfigFromEnv()
	assert.Equal(t, "latency_priority", config.Router.DefaultStage)
}

func TestConfig_OpenCodeWorkerExtract(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	config, _ := LoadConfigFromEnv()

	// If real opencode config exists and has the provider/model format,
	// provider/protocol should equal the parsed provider name.
	// If no config found, falls through to system env scan.
	// Either way: provider and protocol should match (both non-empty when enabled).
	if config.Enabled {
		assert.Equal(t, config.Model.Provider, config.Model.Protocol,
			"Provider and Protocol should match for opencode")
		assert.NotEmpty(t, config.Model.Model)
		assert.NotEmpty(t, config.Model.Provider)
	}
}

// ========================================
// recordMetrics with real timer
// ========================================

func TestEnhancedBrainWrapper_RecordMetrics_WithRealTimer(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "response text", nil
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	result, err := wrapper.Chat(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "response text", result)

	stats := metricsCollector.GetStats()
	assert.Greater(t, stats.TotalRequests, int64(0))
}

func TestEnhancedBrainWrapper_RecordMetrics_ErrorPath(t *testing.T) {
	expectedErr := fmt.Errorf("chat error")
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "", expectedErr
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	_, err := wrapper.Chat(context.Background(), "test")
	assert.Error(t, err)

	stats := metricsCollector.GetStats()
	// Error requests still get recorded
	assert.Greater(t, stats.TotalRequests, int64(0))
}

func TestParseRouterModels_InvalidCostFields(t *testing.T) {
	// Non-numeric cost fields should default to 0
	result := parseRouterModels("model:provider:notanumber:notanumber:notanumber")
	require.Len(t, result, 1)
	assert.Equal(t, "model", result[0].Name)
	assert.Equal(t, float64(0), result[0].CostPer1KInput)
	assert.Equal(t, float64(0), result[0].CostPer1KOutput)
	assert.Equal(t, int64(0), result[0].AvgLatencyMs)
}

func TestParseRouterModels_EmptyParts(t *testing.T) {
	// Empty parts between semicolons should be skipped
	result := parseRouterModels(";model:provider:0.01:0.02:100;;")
	require.Len(t, result, 1)
	assert.Equal(t, "model", result[0].Name)
}

func TestParseRouterModels_MultipleValidAndInvalid(t *testing.T) {
	result := parseRouterModels("valid:prov:0.01:0.02:100;invalid;another:prov:0.03:0.04:200")
	require.Len(t, result, 2)
	assert.Equal(t, "valid", result[0].Name)
	assert.Equal(t, "another", result[1].Name)
}
