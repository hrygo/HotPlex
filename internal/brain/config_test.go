package brain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAndValidate_Defaults(t *testing.T) {
	cfg, errs := LoadAndValidate()
	require.Empty(t, errs, "defaults should produce no validation errors")

	assert.Equal(t, 30, cfg.Model.TimeoutS)
	assert.Equal(t, true, cfg.Cache.Enabled)
	assert.Equal(t, 1000, cfg.Cache.Size)
	assert.Equal(t, true, cfg.Retry.Enabled)
	assert.Equal(t, 3, cfg.Retry.MaxAttempts)
	assert.Equal(t, 100, cfg.Retry.MinWaitMs)
	assert.Equal(t, 5000, cfg.Retry.MaxWaitMs)
	assert.Equal(t, true, cfg.Metrics.Enabled)
	assert.Equal(t, "hotplex-brain", cfg.Metrics.ServiceName)
	assert.Equal(t, 10*time.Second, cfg.Metrics.ExportInterval)
	assert.Equal(t, true, cfg.Cost.Enabled)
	assert.Equal(t, false, cfg.Cost.EnableBudget)
	assert.Equal(t, false, cfg.RateLimit.Enabled)
	assert.Equal(t, 10.0, cfg.RateLimit.RPS)
	assert.Equal(t, 20, cfg.RateLimit.Burst)
	assert.Equal(t, 100, cfg.RateLimit.QueueSize)
	assert.Equal(t, 30*time.Second, cfg.RateLimit.QueueTimeout)
	assert.Equal(t, false, cfg.RateLimit.PerModel)
	assert.Equal(t, false, cfg.Router.Enabled)
	assert.Equal(t, "cost_priority", cfg.Router.DefaultStage)
	assert.Nil(t, cfg.Router.Models)
	assert.Equal(t, false, cfg.CircuitBreaker.Enabled)
	assert.Equal(t, 5, cfg.CircuitBreaker.MaxFailures)
	assert.Equal(t, 30*time.Second, cfg.CircuitBreaker.Timeout)
	assert.Equal(t, 60*time.Second, cfg.CircuitBreaker.Interval)
}

func TestLoadAndValidate_EnvOverrides(t *testing.T) {

	envSets := map[string]string{
		"HOTPLEX_BRAIN_TIMEOUT_S":                    "60",
		"HOTPLEX_BRAIN_CACHE_SIZE":                   "500",
		"HOTPLEX_BRAIN_MAX_RETRIES":                  "5",
		"HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS":            "200",
		"HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS":            "10000",
		"HOTPLEX_BRAIN_METRICS_ENABLED":              "false",
		"HOTPLEX_BRAIN_METRICS_SERVICE_NAME":         "test-svc",
		"HOTPLEX_BRAIN_METRICS_EXPORT_INTERVAL":      "30s",
		"HOTPLEX_BRAIN_COST_TRACKING_ENABLED":        "false",
		"HOTPLEX_BRAIN_COST_ENABLE_BUDGET":           "true",
		"HOTPLEX_BRAIN_RATE_LIMIT_ENABLED":           "true",
		"HOTPLEX_BRAIN_RATE_LIMIT_RPS":               "50.5",
		"HOTPLEX_BRAIN_RATE_LIMIT_BURST":             "100",
		"HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE":        "200",
		"HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT":     "1m",
		"HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL":         "true",
		"HOTPLEX_BRAIN_ROUTER_ENABLED":               "true",
		"HOTPLEX_BRAIN_ROUTER_STRATEGY":              "latency_priority",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED":      "true",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES": "10",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT":      "45s",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL":     "2m",
	}
	for k, v := range envSets {
		t.Setenv(k, v)
	}

	cfg, errs := LoadAndValidate()
	require.Empty(t, errs)

	assert.Equal(t, 60, cfg.Model.TimeoutS)
	assert.Equal(t, 500, cfg.Cache.Size)
	assert.Equal(t, 5, cfg.Retry.MaxAttempts)
	assert.Equal(t, 200, cfg.Retry.MinWaitMs)
	assert.Equal(t, 10000, cfg.Retry.MaxWaitMs)
	assert.False(t, cfg.Metrics.Enabled)
	assert.Equal(t, "test-svc", cfg.Metrics.ServiceName)
	assert.Equal(t, 30*time.Second, cfg.Metrics.ExportInterval)
	assert.False(t, cfg.Cost.Enabled)
	assert.True(t, cfg.Cost.EnableBudget)
	assert.True(t, cfg.RateLimit.Enabled)
	assert.Equal(t, 50.5, cfg.RateLimit.RPS)
	assert.Equal(t, 100, cfg.RateLimit.Burst)
	assert.Equal(t, 200, cfg.RateLimit.QueueSize)
	assert.Equal(t, time.Minute, cfg.RateLimit.QueueTimeout)
	assert.True(t, cfg.RateLimit.PerModel)
	assert.True(t, cfg.Router.Enabled)
	assert.Equal(t, "latency_priority", cfg.Router.DefaultStage)
	assert.True(t, cfg.CircuitBreaker.Enabled)
	assert.Equal(t, 10, cfg.CircuitBreaker.MaxFailures)
	assert.Equal(t, 45*time.Second, cfg.CircuitBreaker.Timeout)
	assert.Equal(t, 2*time.Minute, cfg.CircuitBreaker.Interval)
}

func TestLoadAndValidate_InvalidValuesFallBack(t *testing.T) {

	t.Setenv("HOTPLEX_BRAIN_TIMEOUT_S", "-1")
	t.Setenv("HOTPLEX_BRAIN_CACHE_SIZE", "abc")
	t.Setenv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES", "0")

	cfg, errs := LoadAndValidate()

	require.Len(t, errs, 3, "should report one error per invalid env var")

	assert.Equal(t, 30, cfg.Model.TimeoutS, "negative timeout should fall back to default 30")
	assert.Equal(t, 1000, cfg.Cache.Size, "non-integer should fall back to default 1000")
	assert.Equal(t, 5, cfg.CircuitBreaker.MaxFailures, "zero max failures should fall back")
}

func TestLoadAndValidate_DurationParsing(t *testing.T) {
	_, errs := LoadAndValidate()
	require.Empty(t, errs)
}

func TestConfigRegistry_Coverage(t *testing.T) {
	// Verify every spec has a non-empty name and env key.
	for _, spec := range configRegistry {
		assert.NotEmpty(t, spec.Name, "spec should have a name")
		assert.NotEmpty(t, spec.EnvKey, "spec %s should have an env key", spec.Name)
		if spec.Validate != nil {
			assert.NoError(t, spec.Validate(spec.Default),
				"spec %s: default %q should pass validation", spec.Name, spec.Default)
		}
	}
}

func TestValidationHelpers(t *testing.T) {

	assert.NoError(t, positiveInt("1"))
	assert.Error(t, positiveInt("0"))
	assert.Error(t, positiveInt("-1"))
	assert.Error(t, positiveInt("abc"))

	assert.NoError(t, nonNegativeInt("0"))
	assert.NoError(t, nonNegativeInt("5"))
	assert.Error(t, nonNegativeInt("-1"))

	assert.NoError(t, nonNegativeFloat("0"))
	assert.NoError(t, nonNegativeFloat("3.14"))
	assert.Error(t, nonNegativeFloat("-0.1"))

	assert.NoError(t, positiveDuration("1s"))
	assert.NoError(t, positiveDuration("1"))
	assert.Error(t, positiveDuration("0"))
	assert.Error(t, positiveDuration("0s"))

}
