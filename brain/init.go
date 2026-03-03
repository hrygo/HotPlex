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

		// === Phase 2: Initialize observability components ===

		// Initialize metrics collector
		var metricsCollector *llm.MetricsCollector
		if config.MetricsEnabled {
			metricsCollector = llm.NewMetricsCollector(llm.MetricsConfig{
				Enabled:           true,
				ServiceName:       config.MetricsServiceName,
				MaxLatencySamples: 1000,
			})
			logger.Info("Metrics collection enabled", "service", config.MetricsServiceName)
		}

		// Initialize cost calculator
		var costCalculator *llm.CostCalculator
		if config.CostTrackingEnabled {
			costCalculator = llm.NewCostCalculator()
			logger.Info("Cost tracking enabled")
		}

		// Initialize rate limiter
		var rateLimiter *llm.RateLimiter
		if config.RateLimitEnabled {
			rateLimiter = llm.NewRateLimiter(llm.RateLimitConfig{
				RequestsPerSecond: config.RateLimitRPS,
				BurstSize:         config.RateLimitBurst,
				MaxQueueSize:      config.RateLimitQueueSize,
				QueueTimeout:      config.RateLimitQueueTimeout,
				PerModel:          config.RateLimitPerModel,
			})
			logger.Info("Rate limiting enabled",
				"rps", config.RateLimitRPS,
				"burst", config.RateLimitBurst,
				"queue_size", config.RateLimitQueueSize)
		}

		// Initialize model router
		var router *llm.Router
		if config.RouterEnabled {
			modelConfigs := config.ParseRouterModels()
			if len(modelConfigs) == 0 {
				// Use default models if not configured (convert from pricing to config)
				pricing := llm.DefaultModelPricing()
				for _, p := range pricing {
					modelConfigs = append(modelConfigs, llm.ModelConfig{
						Name:            p.ModelName,
						Provider:        p.Provider,
						CostPer1KInput:  p.CostPer1KInput,
						CostPer1KOutput: p.CostPer1KOutput,
						Enabled:         true,
					})
				}
			}

			router = llm.NewRouter(llm.RouterConfig{
				DefaultStrategy:    llm.RouteStrategy(config.RouterStrategy),
				Models:             modelConfigs,
				ScenarioModelMap:   make(map[llm.Scenario]string),
				FallbackModel:      config.Model,
				Logger:             logger,
			}, metricsCollector)

			logger.Info("Model routing enabled",
				"strategy", config.RouterStrategy,
				"models", len(modelConfigs))
		}

		// Wrap with production features: retry, cache, streaming
		client = llm.NewRetryClient(baseClient, config.MaxRetries, config.RetryMinWaitMs, config.RetryMaxWaitMs)

		if config.CacheSize > 0 {
			client = llm.NewCachedClient(client, config.CacheSize)
		}

		// Wrap with rate limiting if enabled
		if rateLimiter != nil {
			client = llm.NewRateLimitedClient(client, rateLimiter)
		}

		// Create enhanced brain wrapper with all Phase 2 features
		SetGlobal(&enhancedBrainWrapper{
			client:         client,
			config:         config,
			metrics:        metricsCollector,
			costCalculator: costCalculator,
			router:         router,
			rateLimiter:    rateLimiter,
			logger:         logger,
		})

		logger.Info("Native Brain initialized (Phase 2)",
			"provider", config.Provider,
			"model", config.Model,
			"timeout_s", config.TimeoutS,
			"cache_size", config.CacheSize,
			"max_retries", config.MaxRetries,
			"metrics_enabled", config.MetricsEnabled,
			"cost_tracking_enabled", config.CostTrackingEnabled,
			"rate_limit_enabled", config.RateLimitEnabled,
			"router_enabled", config.RouterEnabled)
	default:
		// Fallback for unknown provider
		logger.Warn("Unknown brain provider specified. Brain disabled.", "provider", config.Provider)
	}

	return nil
}

// enhancedBrainWrapper satisfies Brain, StreamingBrain, RoutableBrain, and ObservableBrain interfaces.
type enhancedBrainWrapper struct {
	client interface {
		Chat(ctx context.Context, prompt string) (string, error)
		Analyze(ctx context.Context, prompt string, target any) error
		ChatStream(ctx context.Context, prompt string) (<-chan string, error)
		HealthCheck(ctx context.Context) HealthStatus
	}
	config         Config
	metrics        *llm.MetricsCollector
	costCalculator *llm.CostCalculator
	router         *llm.Router
	rateLimiter    *llm.RateLimiter
	logger         *slog.Logger
}

func (w *enhancedBrainWrapper) Chat(ctx context.Context, prompt string) (string, error) {
	return w.ChatWithModel(ctx, "", prompt)
}

func (w *enhancedBrainWrapper) Analyze(ctx context.Context, prompt string, target any) error {
	return w.AnalyzeWithModel(ctx, "", prompt, target)
}

func (w *enhancedBrainWrapper) ChatWithModel(ctx context.Context, model string, prompt string) (string, error) {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}

	// Select model via router if not specified
	if model == "" && w.router != nil {
		scenario := w.router.DetectScenario(prompt)
		strategy := llm.StrategyCostPriority // Default strategy
		if w.router.GetDefaultStrategy() != "" {
			strategy = w.router.GetDefaultStrategy()
		}
		selectedModel, err := w.router.SelectModel(ctx, scenario, strategy)
		if err == nil {
			model = selectedModel.Name
		} else if w.logger != nil {
			w.logger.Warn("Model selection failed, using default", "error", err)
		}
	}

	// Use default model if still empty
	if model == "" {
		model = w.config.Model
	}

	// Apply rate limiting
	if w.rateLimiter != nil {
		if err := w.rateLimiter.WaitModel(ctx, model); err != nil {
			return "", err
		}
	}

	// Start metrics timer
	var timer *llm.RequestTimer
	if w.metrics != nil {
		timer = llm.NewRequestTimer(w.metrics, model, "chat")
	}

	// Execute request
	result, err := w.client.Chat(ctx, prompt)

	// Record metrics
	if timer != nil {
		inputTokens := w.costCalculator.CountTokens(prompt)
		outputTokens := w.costCalculator.CountTokens(result)
		cost := 0.0
		if w.costCalculator != nil {
			cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
			_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, outputTokens)
		}
		timer.Record(int64(inputTokens), int64(outputTokens), cost, err)
	}

	return result, err
}

func (w *enhancedBrainWrapper) AnalyzeWithModel(ctx context.Context, model string, prompt string, target any) error {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}

	// Select model via router if not specified
	if model == "" && w.router != nil {
		scenario := llm.ScenarioAnalyze
		strategy := llm.StrategyCostPriority // Default strategy
		if w.router.GetDefaultStrategy() != "" {
			strategy = w.router.GetDefaultStrategy()
		}
		selectedModel, err := w.router.SelectModel(ctx, scenario, strategy)
		if err == nil {
			model = selectedModel.Name
		} else if w.logger != nil {
			w.logger.Warn("Model selection failed, using default", "error", err)
		}
	}

	// Use default model if still empty
	if model == "" {
		model = w.config.Model
	}

	// Apply rate limiting
	if w.rateLimiter != nil {
		if err := w.rateLimiter.WaitModel(ctx, model); err != nil {
			return err
		}
	}

	// Start metrics timer
	var timer *llm.RequestTimer
	if w.metrics != nil {
		timer = llm.NewRequestTimer(w.metrics, model, "analyze")
	}

	// Execute request
	err := w.client.Analyze(ctx, prompt, target)

	// Record metrics
	if timer != nil {
		inputTokens := w.costCalculator.CountTokens(prompt)
		outputTokens := 100 // Estimate for structured output
		cost := 0.0
		if w.costCalculator != nil {
			cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
			_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, outputTokens)
		}
		timer.Record(int64(inputTokens), int64(outputTokens), cost, err)
	}

	return err
}

func (w *enhancedBrainWrapper) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	// Apply timeout from config
	if w.config.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.config.TimeoutS)*time.Second)
		defer cancel()
	}

	// Apply rate limiting
	if w.rateLimiter != nil {
		if err := w.rateLimiter.WaitModel(ctx, w.config.Model); err != nil {
			return nil, err
		}
	}

	return w.client.ChatStream(ctx, prompt)
}

func (w *enhancedBrainWrapper) HealthCheck(ctx context.Context) HealthStatus {
	return w.client.HealthCheck(ctx)
}

func (w *enhancedBrainWrapper) GetMetrics() llm.MetricsStats {
	if w.metrics == nil {
		return llm.MetricsStats{}
	}
	return w.metrics.GetStats()
}

func (w *enhancedBrainWrapper) GetCostCalculator() *llm.CostCalculator {
	return w.costCalculator
}

func (w *enhancedBrainWrapper) GetRouter() *llm.Router {
	return w.router
}

func (w *enhancedBrainWrapper) GetRateLimiter() *llm.RateLimiter {
	return w.rateLimiter
}
