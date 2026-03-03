package brain

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/brain/llm"
)

// Init initializes the global Brain from environmental variables.
// It detects the provider and sets the Global Brain instance.
func Init(logger *slog.Logger) error {
	config := LoadConfigFromEnv()

	if !config.Enabled {
		logger.Debug("Native Brain is disabled or missing configuration. Skipping.")
		return nil
	}

	switch config.Provider {
	case "openai":
		// This uses OpenAI SDK for OpenAI, DeepSeek, Groq, etc.
		var client interface {
			Chat(ctx context.Context, prompt string) (string, error)
			Analyze(ctx context.Context, prompt string, target any) error
			ChatStream(ctx context.Context, prompt string) (<-chan string, error)
			HealthCheck(ctx context.Context) llm.HealthStatus
		}
		
		baseClient := llm.NewOpenAIClient(config.APIKey, config.Endpoint, config.Model, logger)
		
		// Wrap with production features: retry, cache, streaming
		client = llm.NewRetryClient(baseClient, config.MaxRetries, config.RetryMinWaitMs, config.RetryMaxWaitMs)
		
		if config.CacheSize > 0 {
			client = llm.NewCachedClient(client, config.CacheSize)
		}
		
		SetGlobal(&brainWrapper{client: client, config: config})
		logger.Info("Native Brain initialized", 
			"provider", config.Provider, 
			"model", config.Model,
			"timeout_s", config.TimeoutS,
			"cache_size", config.CacheSize,
			"max_retries", config.MaxRetries)
	default:
		// Fallback for unknown provider
		logger.Warn("Unknown brain provider specified. Brain disabled.", "provider", config.Provider)
	}

	return nil
}

// brainWrapper satisfies the Brain and StreamingBrain interfaces.
type brainWrapper struct {
	client interface {
		Chat(ctx context.Context, prompt string) (string, error)
		Analyze(ctx context.Context, prompt string, target any) error
		ChatStream(ctx context.Context, prompt string) (<-chan string, error)
		HealthCheck(ctx context.Context) HealthStatus
	}
	config Config
}

func (w *brainWrapper) Chat(ctx context.Context, prompt string) (string, error) {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}
	return w.client.Chat(ctx, prompt)
}

func (w *brainWrapper) Analyze(ctx context.Context, prompt string, target any) error {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}
	return w.client.Analyze(ctx, prompt, target)
}

func (w *brainWrapper) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}
	return w.client.ChatStream(ctx, prompt)
}

func (w *brainWrapper) HealthCheck(ctx context.Context) HealthStatus {
	return w.client.HealthCheck(ctx)
}
