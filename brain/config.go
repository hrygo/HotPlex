package brain

import (
	"os"
	"strconv"
)

// Config holds the configuration for the Global Brain.
type Config struct {
	// Enabled is automatically determined based on APIKey presence.
	Enabled bool
	// Provider supports "openai" (default), "anthropic", "gemini".
	Provider string
	// APIKey is the secret for accessing the provider API.
	APIKey string
	// Endpoint is the optional base URL for the API (e.g. for DeepSeek/Groq).
	Endpoint string
	// Model is the specific model to use (default: gpt-4o-mini).
	Model string
	// Timeout is the maximum duration for a brain request.
	// Defaults to 10 seconds for standard requests.
	TimeoutS int
	// CacheSize is the LRU cache capacity (default: 1000 entries).
	// Set to 0 to disable caching.
	CacheSize int
	// MaxRetries is the maximum number of retry attempts (default: 3).
	// Set to 0 to disable retries.
	MaxRetries int
	// RetryMinWait is the minimum wait time between retries (default: 100ms).
	RetryMinWaitMs int
	// RetryMaxWait is the maximum wait time between retries (default: 5s).
	RetryMaxWaitMs int
}

// LoadConfigFromEnv loads the brain configuration from environment variables.
func LoadConfigFromEnv() Config {
	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")

	return Config{
		Enabled:        apiKey != "",
		Provider:       getEnv("HOTPLEX_BRAIN_PROVIDER", "openai"),
		APIKey:         apiKey,
		Endpoint:       os.Getenv("HOTPLEX_BRAIN_ENDPOINT"),
		Model:          getEnv("HOTPLEX_BRAIN_MODEL", "gpt-4o-mini"),
		TimeoutS:       getIntEnv("HOTPLEX_BRAIN_TIMEOUT_S", 10),
		CacheSize:      getIntEnv("HOTPLEX_BRAIN_CACHE_SIZE", 1000),
		MaxRetries:     getIntEnv("HOTPLEX_BRAIN_MAX_RETRIES", 3),
		RetryMinWaitMs: getIntEnv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", 100),
		RetryMaxWaitMs: getIntEnv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", 5000),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}
