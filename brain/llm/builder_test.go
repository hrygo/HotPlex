package llm

import (
	"testing"
)

func TestClientBuilder_RequiresAPIKey(t *testing.T) {
	_, err := NewClientBuilder().
		WithModel("gpt-4").
		Build()

	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestClientBuilder_RequiresModel(t *testing.T) {
	_, err := NewClientBuilder().
		WithAPIKey("test-key").
		Build()

	if err == nil {
		t.Error("expected error for missing model")
	}
}

func TestClientBuilder_MinimalClient(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Error("expected non-nil client")
	}

	// Verify it's an OpenAIClient (no wrappers)
	_, ok := client.(*OpenAIClient)
	if !ok {
		t.Error("expected OpenAIClient without wrappers")
	}
}

func TestClientBuilder_WithCache(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCache().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CachedClient
	_, ok := client.(*CachedClient)
	if !ok {
		t.Error("expected CachedClient wrapper")
	}
}

func TestClientBuilder_WithCacheSize(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCacheSize(500).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CachedClient
	_, ok := client.(*CachedClient)
	if !ok {
		t.Error("expected CachedClient wrapper")
	}
}

func TestClientBuilder_WithRetry(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRetry(3).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RetryClient
	_, ok := client.(*RetryClient)
	if !ok {
		t.Error("expected RetryClient wrapper")
	}
}

func TestClientBuilder_WithRetryConfig(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRetryConfig(RetryConfig{
			MaxRetries: 5,
			MinWaitMs:  100,
			MaxWaitMs:  1000,
		}).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RetryClient
	_, ok := client.(*RetryClient)
	if !ok {
		t.Error("expected RetryClient wrapper")
	}
}

func TestClientBuilder_WithRateLimit(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRateLimit(100).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RateLimitedClient
	_, ok := client.(*RateLimitedClient)
	if !ok {
		t.Error("expected RateLimitedClient wrapper")
	}
}

func TestClientBuilder_WithCircuitBreaker(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCircuitBreaker().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CircuitClient
	_, ok := client.(*CircuitClient)
	if !ok {
		t.Error("expected CircuitClient wrapper")
	}
}

func TestClientBuilder_WithMetrics(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithMetrics().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a MetricsClient (outermost wrapper)
	_, ok := client.(*MetricsClient)
	if !ok {
		t.Error("expected MetricsClient wrapper")
	}
}

func TestClientBuilder_FullStack(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithMetrics().
		WithCache().
		WithCircuitBreaker().
		WithRateLimit(100).
		WithRetry(3).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the wrapping order:
	// Metrics (outermost) -> Circuit -> RateLimit -> Retry -> Cache (innermost)
	mc, ok := client.(*MetricsClient)
	if !ok {
		t.Fatal("expected MetricsClient as outermost wrapper")
	}

	cc, ok := mc.client.(*CircuitClient)
	if !ok {
		t.Fatal("expected CircuitClient as second wrapper")
	}

	rlc, ok := cc.client.(*RateLimitedClient)
	if !ok {
		t.Fatal("expected RateLimitedClient as third wrapper")
	}

	rc, ok := rlc.client.(*RetryClient)
	if !ok {
		t.Fatal("expected RetryClient as fourth wrapper")
	}

	_, ok = rc.client.(*CachedClient)
	if !ok {
		t.Fatal("expected CachedClient as innermost wrapper before base")
	}
}

func TestClientBuilder_WithEndpoint(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithEndpoint("https://api.deepseek.com/v1").
		WithModel("deepseek-chat").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestDefaultConfigs(t *testing.T) {
	// Test DefaultMetricsConfig
	metricsCfg := DefaultMetricsConfig()
	if !metricsCfg.Enabled {
		t.Error("expected metrics to be enabled by default")
	}

	// Test DefaultCacheConfig
	cacheCfg := DefaultCacheConfig()
	if cacheCfg.Size != DefaultCacheSize {
		t.Errorf("expected cache size %d, got %d", DefaultCacheSize, cacheCfg.Size)
	}

	// Test DefaultRetryConfig
	retryCfg := DefaultRetryConfig()
	if retryCfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected max retries %d, got %d", DefaultMaxRetries, retryCfg.MaxRetries)
	}

	// Test DefaultRateLimitConfig
	rateLimitCfg := DefaultRateLimitConfig()
	if rateLimitCfg.RequestsPerSecond != DefaultRPS {
		t.Errorf("expected RPS %f, got %f", DefaultRPS, rateLimitCfg.RequestsPerSecond)
	}
}

func TestClientBuilder_FluentChaining(t *testing.T) {
	// Verify that all With* methods return the builder for chaining
	builder := NewClientBuilder()

	if builder.WithAPIKey("key") != builder {
		t.Error("WithAPIKey should return builder")
	}
	if builder.WithEndpoint("endpoint") != builder {
		t.Error("WithEndpoint should return builder")
	}
	if builder.WithModel("model") != builder {
		t.Error("WithModel should return builder")
	}
	if builder.WithMetrics() != builder {
		t.Error("WithMetrics should return builder")
	}
	if builder.WithCache() != builder {
		t.Error("WithCache should return builder")
	}
	if builder.WithRetry(3) != builder {
		t.Error("WithRetry should return builder")
	}
	if builder.WithCircuitBreaker() != builder {
		t.Error("WithCircuitBreaker should return builder")
	}
	if builder.WithRateLimit(100) != builder {
		t.Error("WithRateLimit should return builder")
	}
}
