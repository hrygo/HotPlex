package brain

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// Init initializes the global Brain from environmental variables.
// It detects the provider and sets the Global Brain instance.
//
// If HOTPLEX_BRAIN_API_KEY is not set, Brain is disabled and features gracefully degrade.
func Init(logger *slog.Logger) error {
	config, validationErrs := LoadConfigFromEnv()

	for _, err := range validationErrs {
		logger.Warn("Brain config validation warning", "error", err)
	}

	if !config.Enabled {
		logger.Debug("Native Brain is disabled or missing configuration. Skipping.")
		return nil
	}

	var baseClient llm.LLMClient

	// 1. Initialize base client based on Protocol
	switch config.Model.Protocol {
	case "anthropic":
		baseClient = llm.NewAnthropicClient(config.Model.APIKey, config.Model.Endpoint, config.Model.Model, logger)
		logger.Info("Anthropic brain client initialized", "model", config.Model.Model)
	default:
		// "openai" and any unknown protocol default to OpenAI-compatible API.
		baseClient = llm.NewOpenAIClient(config.Model.APIKey, config.Model.Endpoint, config.Model.Model, logger)
		logger.Debug("OpenAI brain client initialized", "model", config.Model.Model)
	}

	// 2. Initialize orchestration & observability components (shared)
	var metricsCollector *llm.MetricsCollector
	if config.Metrics.Enabled {
		metricsCollector = llm.NewMetricsCollector(llm.MetricsConfig{
			Enabled:           true,
			ServiceName:       config.Metrics.ServiceName,
			MaxLatencySamples: 1000,
		})
	}

	var costCalculator *llm.CostCalculator
	if config.Cost.Enabled {
		costCalculator = llm.NewCostCalculator()
	}

	var rateLimiter *llm.RateLimiter
	if config.RateLimit.Enabled {
		rateLimiter = llm.NewRateLimiter(llm.RateLimitConfig{
			RequestsPerSecond: config.RateLimit.RPS,
			BurstSize:         config.RateLimit.Burst,
			MaxQueueSize:      config.RateLimit.QueueSize,
			QueueTimeout:      config.RateLimit.QueueTimeout,
			PerModel:          config.RateLimit.PerModel,
		})
	}

	var circuitBreaker *llm.CircuitBreaker
	if config.CircuitBreaker.Enabled {
		circuitBreaker = llm.NewCircuitBreaker(llm.CircuitBreakerConfig{
			Name:        "brain",
			MaxFailures: uint32(config.CircuitBreaker.MaxFailures),
			Interval:    config.CircuitBreaker.Interval,
			Timeout:     config.CircuitBreaker.Timeout,
			Logger:      logger,
		})
	}

	var router *llm.Router
	if config.Router.Enabled {
		modelConfigs := config.Router.Models
		if len(modelConfigs) == 0 {
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
			DefaultStrategy:  llm.RouteStrategy(config.Router.DefaultStage),
			Models:           modelConfigs,
			ScenarioModelMap: make(map[llm.Scenario]string),
			FallbackModel:    config.Model.Model,
			Logger:           logger,
		}, metricsCollector)
	}

	// 3. Apply shared middleware wrapping
	client := baseClient

	// Retry
	client = llm.NewRetryClient(client, config.Retry.MaxAttempts, config.Retry.MinWaitMs, config.Retry.MaxWaitMs)

	// Cache
	if config.Cache.Enabled && config.Cache.Size > 0 {
		client = llm.NewCachedClient(client, config.Cache.Size)
	}

	// Rate limiting handled by wrapper.applyRateLimit (not as a decorator,
	// to avoid double rate limiting with provider-built-in limits).

	// 4. Register global brain instance
	SetGlobal(&enhancedBrainWrapper{
		client:         client,
		config:         config,
		metrics:        metricsCollector,
		costCalculator: costCalculator,
		router:         router,
		rateLimiter:    rateLimiter,
		circuitBreaker: circuitBreaker,
		logger:         logger,
		timeout:        time.Duration(config.Model.TimeoutS) * time.Second, // Pre-compute timeout
	})

	// 5. Log initialization result
	logger.Info("Native Brain initialized",
		"provider", config.Model.Provider,
		"protocol", config.Model.Protocol,
		"model", config.Model.Model,
		"cache", config.Cache.Enabled,
		"metrics", config.Metrics.Enabled)

	return nil
}

// enhancedBrainWrapper implements the Brain interface.
type enhancedBrainWrapper struct {
	client         llm.LLMClient
	config         Config
	metrics        *llm.MetricsCollector
	costCalculator *llm.CostCalculator
	router         *llm.Router
	rateLimiter    *llm.RateLimiter
	circuitBreaker *llm.CircuitBreaker
	logger         *slog.Logger
	timeout        time.Duration // Pre-computed timeout for hot path
}

func (w *enhancedBrainWrapper) Chat(ctx context.Context, prompt string) (string, error) {
	return w.ChatWithOptions(ctx, prompt, llm.ChatOptions{})
}

func (w *enhancedBrainWrapper) ChatWithOptions(ctx context.Context, prompt string, opts llm.ChatOptions) (string, error) {
	ctx, cancel := w.applyTimeout(ctx)
	defer cancel()

	model := w.selectModel(ctx, "", llm.ScenarioChat)

	if err := w.applyRateLimit(ctx, model); err != nil {
		return "", err
	}

	timer := w.startMetricsTimer(ctx, model, "chat")

	var result string
	var err error
	if w.circuitBreaker != nil {
		cbErr := w.circuitBreaker.Execute(ctx, func() error {
			result, err = w.client.ChatWithOptions(ctx, prompt, opts)
			return err
		})
		if cbErr != nil {
			err = cbErr
		}
	} else {
		result, err = w.client.ChatWithOptions(ctx, prompt, opts)
	}

	w.recordMetrics(timer, model, prompt, result, err)

	return result, err
}

func (w *enhancedBrainWrapper) Analyze(ctx context.Context, prompt string, target any) error {
	return w.AnalyzeWithModel(ctx, "", prompt, target)
}

func (w *enhancedBrainWrapper) ChatWithModel(ctx context.Context, model, prompt string) (string, error) {
	ctx, cancel := w.applyTimeout(ctx)
	defer cancel()

	model = w.selectModel(ctx, model, llm.ScenarioChat)

	if err := w.applyRateLimit(ctx, model); err != nil {
		return "", err
	}

	timer := w.startMetricsTimer(ctx, model, "chat")
	result, err := w.client.Chat(ctx, prompt)
	w.recordMetrics(timer, model, prompt, result, err)

	return result, err
}

func (w *enhancedBrainWrapper) AnalyzeWithModel(ctx context.Context, model, prompt string, target any) error {
	ctx, cancel := w.applyTimeout(ctx)
	defer cancel()

	model = w.selectModel(ctx, model, llm.ScenarioAnalyze)

	if err := w.applyRateLimit(ctx, model); err != nil {
		return err
	}

	timer := w.startMetricsTimer(ctx, model, "analyze")
	err := w.client.Analyze(ctx, prompt, target)
	w.recordMetricsForAnalyze(timer, model, prompt, err)

	return err
}

// applyTimeout applies the configured timeout to the context.
// Returns the timeout context and its cancel function.
// The caller must arrange for cancel to be called (typically via defer).
func (w *enhancedBrainWrapper) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if w.timeout > 0 {
		return context.WithTimeout(ctx, w.timeout)
	}
	return ctx, func() {}
}

// selectModel selects a model using the router, or falls back to default.
func (w *enhancedBrainWrapper) selectModel(ctx context.Context, model string, scenario llm.Scenario) string {
	if model != "" || w.router == nil {
		if model == "" {
			model = w.config.Model.Model
		}
		return model
	}

	if scenario == "" {
		scenario = w.router.DetectScenario("")
	}

	strategy := llm.StrategyCostPriority
	if w.router.GetDefaultStrategy() != "" {
		strategy = w.router.GetDefaultStrategy()
	}

	selectedModel, err := w.router.SelectModel(ctx, scenario, strategy)
	if err == nil {
		model = selectedModel.Name
	} else if w.logger != nil {
		w.logger.Warn("Model selection failed, using default", "error", err)
		model = w.config.Model.Model
	}

	return model
}

// applyRateLimit applies rate limiting for the specified model.
func (w *enhancedBrainWrapper) applyRateLimit(ctx context.Context, model string) error {
	if w.rateLimiter != nil {
		return w.rateLimiter.WaitModel(ctx, model)
	}
	return nil
}

// startMetricsTimer starts a metrics timer for the given model and operation.
func (w *enhancedBrainWrapper) startMetricsTimer(ctx context.Context, model, operation string) *llm.RequestTimer {
	if w.metrics != nil {
		return llm.NewRequestTimer(ctx, w.metrics, model, operation)
	}
	return nil
}

// recordMetrics records metrics for a chat operation.
func (w *enhancedBrainWrapper) recordMetrics(timer *llm.RequestTimer, model, prompt, result string, err error) {
	if timer == nil {
		return
	}

	inputTokens := 0
	outputTokens := 0
	cost := 0.0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
		outputTokens = w.costCalculator.CountTokens(result)
		cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
		_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, outputTokens)
	}
	timer.Record(int64(inputTokens), int64(outputTokens), cost, err)
}

// recordMetricsForAnalyze records metrics for an analyze operation.
func (w *enhancedBrainWrapper) recordMetricsForAnalyze(timer *llm.RequestTimer, model, prompt string, err error) {
	if timer == nil {
		return
	}

	inputTokens := 0
	cost := 0.0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
		cost, _ = w.costCalculator.CalculateCost(model, inputTokens, 100)
		_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, 100)
	}
	timer.Record(int64(inputTokens), 100, cost, err)
}

func (w *enhancedBrainWrapper) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	// Apply pre-computed timeout. cancel is deferred inside the goroutine
	// so the timeout context outlives this function call.
	var streamCancel context.CancelFunc
	if w.timeout > 0 {
		ctx, streamCancel = context.WithTimeout(ctx, w.timeout)
	}

	model := w.selectModel(ctx, "", llm.ScenarioChat)
	if err := w.applyRateLimit(ctx, model); err != nil {
		if streamCancel != nil {
			streamCancel()
		}
		return nil, err
	}

	timer := w.startMetricsTimer(ctx, model, "chat_stream")
	inputTokens := 0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
	}

	stream, err := w.client.ChatStream(ctx, prompt)
	if err != nil {
		if streamCancel != nil {
			streamCancel()
		}
		if timer != nil {
			timer.Record(int64(inputTokens), 0, 0, err)
		}
		return nil, err
	}

	outputChan := make(chan string)

	go func() {
		if streamCancel != nil {
			defer streamCancel()
		}
		defer close(outputChan)
		if stream == nil {
			return
		}
		var outputTokens int
		for {
			select {
			case <-ctx.Done():
				return
			case token, ok := <-stream:
				if !ok {
					if timer != nil {
						cost := 0.0
						if w.costCalculator != nil {
							cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
							_, _, _ = w.costCalculator.TrackRequest("stream", model, inputTokens, outputTokens)
						}
						timer.Record(int64(inputTokens), int64(outputTokens), cost, nil)
					}
					return
				}
				if w.costCalculator != nil {
					outputTokens += w.costCalculator.CountTokens(token)
				}
				select {
				case outputChan <- token:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return outputChan, nil
}

func (w *enhancedBrainWrapper) HealthCheck(ctx context.Context) llm.HealthStatus {
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

// Close releases resources held by the brain wrapper.
// Stops the rate limiter's queue-processing goroutine to prevent leaks
// on hot-reload (where a new brain replaces the old one).
func (w *enhancedBrainWrapper) Close() {
	if w.rateLimiter != nil {
		w.rateLimiter.Close()
	}
}
