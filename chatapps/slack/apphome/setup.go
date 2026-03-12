package apphome

import (
	"log/slog"

	"github.com/hrygo/hotplex/brain"
	"github.com/slack-go/slack"
)

// Config holds configuration for the AppHome capability center.
type Config struct {
	// Enabled determines if the capability center is active.
	Enabled bool

	// CapabilitiesPath is the path to the capabilities YAML file.
	CapabilitiesPath string
}

// Setup initializes the AppHome capability center with all components.
// This is the main entry point for integrating AppHome into the Slack adapter.
func Setup(client *slack.Client, b brain.Brain, config Config, logger *slog.Logger) (*Handler, *Registry, *Executor) {
	if !config.Enabled {
		logger.Info("AppHome capability center is disabled")
		return nil, nil, nil
	}

	// Create registry and load capabilities
	registry := NewRegistry(
		WithConfigPath(config.CapabilitiesPath),
		WithLogger(logger),
	)

	// Load capabilities from file
	if config.CapabilitiesPath != "" {
		if err := registry.Load(); err != nil {
			logger.Error("Failed to load capabilities", "error", err)
		}
	} else {
		// Load embedded defaults
		if err := LoadDefaultCapabilities(registry); err != nil {
			logger.Warn("Failed to load default capabilities", "error", err)
		}
	}

	// Create executor
	executor := NewExecutor(
		WithExecutorClient(client),
		WithExecutorLogger(logger),
	)
	if b != nil {
		executor.SetBrain(b)
	}

	// Create handler
	handler := NewHandler(
		registry,
		WithSlackClient(client),
		WithExecutor(executor),
		WithHandlerLogger(logger),
	)

	logger.Info("AppHome capability center initialized",
		"capabilities", registry.Count())

	return handler, registry, executor
}

// LoadDefaultCapabilities loads the embedded default capabilities.
func LoadDefaultCapabilities(registry *Registry) error {
	defaultYAML := `capabilities:
  - id: code_review
    name: 代码审查
    icon: ":mag:"
    description: 对指定文件进行安全/性能/风格审查
    category: code
    enabled: true
    parameters:
      - id: target
        label: 审查目标
        type: text
        required: true
        placeholder: "例如: src/main.go"
    prompt_template: |
      请对以下内容进行代码审查:
      目标: {{.target}}
      请提供具体的改进建议。
    brain_opts:
      intent_confirm: false
      compress_context: true
`
	return registry.LoadFromBytes([]byte(defaultYAML))
}
