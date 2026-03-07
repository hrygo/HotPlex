# HotPlex Providers: AI Agent Abstraction Layer

The `provider` package defines the bridge between HotPlex and various AI CLI agents (e.g., Claude Code, OpenCode). It abstracts platform-specific CLI protocols, event formats, and execution models into a unified interface.

## 🏛 Architecture Overview

The Providers act as **Strategy Adapters** in the HotPlex ecosystem. They handle the low-level details of interacting with different AI agents while exposing a consistent API to the Engine.

```mermaid
graph TD
    Engine[HotPlex Engine] --> Factory[Provider Factory]
    Factory --> Claude[Claude Code Provider]
    Factory --> OpenCode[OpenCode Provider]
    Factory --> Pi[Pi Provider]
    Factory --> Plugin[Plugin Registry]
    Plugin --> Custom[Third-party Providers]

    subgraph Interface [Unified Interface]
        Provider[Provider Interface]
        Event[Normalized Event Model]
    end

    Claude -.-> Provider
    OpenCode -.-> Provider
    Pi -.-> Provider
    Custom -.-> Provider
```

### Key Architectural Concepts

- **`Provider` (Interface)**: The core contract that defines how to start a CLI, send user input, and parse the resulting stream of events.
- **Normalized Event Model**: Regardless of the provider's native output (JSON, SSE, plain text), this package converts it into a standard `ProviderEvent` stream (e.g., `thinking`, `tool_use`, `answer`, `error`).
- **Factory Pattern**: The `ProviderFactory` allows for dynamic registration and creation of providers based on configuration.
- **Plugin System**: Third-party providers can be registered via `RegisterPlugin()` without modifying core code.
- **Protocol Translation**: Each provider implementation handles the specific "dialect" of its underlying CLI.

---

## 🔌 Plugin System (RFC #216)

The plugin system enables third-party extensions without modifying HotPlex core.

### Plugin Interface

```go
type ProviderPlugin interface {
    Type() ProviderType
    New(cfg ProviderConfig, logger *slog.Logger) (Provider, error)
    Meta() ProviderMeta
}
```

### Registration

```go
// external/myprovider/plugin.go
import "github.com/hrygo/hotplex/provider"

type myPlugin struct{}

func (p *myPlugin) Type() provider.ProviderType { return "my-ai" }
func (p *myPlugin) New(cfg provider.ProviderConfig, logger *slog.Logger) (provider.Provider, error) {
    return &myProviderImpl{cfg: cfg, logger: logger}, nil
}
func (p *myPlugin) Meta() provider.ProviderMeta {
    return provider.ProviderMeta{
        Type:        "my-ai",
        DisplayName: "My AI Provider",
        BinaryName:  "my-ai-cli",
        Features: provider.ProviderFeatures{
            SupportsResume:     true,
            SupportsStreamJSON: true,
        },
    }
}

func init() {
    provider.RegisterPlugin(&myPlugin{})
}
```

### Type Checking

```go
// Built-in types
provider.ProviderTypeClaudeCode.IsRegistered() // true
provider.ProviderTypeOpenCode.IsRegistered()   // true

// Plugin types
provider.ProviderType("my-ai").IsRegistered()  // true after plugin registration
```

See `docs/provider-extension-guide.md` for detailed extension guide.

---

## 🛠 Developer Guide

### 1. Implementing a New Provider

To support a new AI CLI tool, implement the `Provider` interface:

```go
type MyNewProvider struct {
    provider.ProviderBase // Optional: provides common functionality
}

func (p *MyNewProvider) Name() string { return "my-new-ai" }

func (p *MyNewProvider) BuildCLIArgs(sessionID string, opts *ProviderSessionOptions) []string {
    // Construct command line arguments (e.g., --session-id, --model)
}

func (p *MyNewProvider) BuildInputMessage(prompt string, taskInst string) (map[string]any, error) {
    // Format the stdin payload for the CLI
}

func (p *MyNewProvider) ParseEvent(line string) ([]*ProviderEvent, error) {
    // Convert a raw line of stdout to normalized events
}

func (p *MyNewProvider) DetectTurnEnd(event *ProviderEvent) bool {
    // Return true when a turn is complete
}

func (p *MyNewProvider) ValidateBinary() (string, error) {
    // Check if CLI binary exists
}

func (p *MyNewProvider) CleanupSession(sessionID string, workDir string) error {
    // Clean up session files
}
```

### 2. Registering with the Factory

Option A - Using Plugin System (Recommended):

```go
func init() {
    provider.RegisterPlugin(&myPlugin{})
}
```

Option B - Direct Factory Registration:

```go
provider.GlobalProviderFactory.Register("my-new-ai", func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
    return &MyNewProvider{...}, nil
})
```

### 3. Using the Provider

```go
pCfg := provider.ProviderConfig{Type: "claude-code", Enabled: true}
prv, err := provider.CreateProvider(pCfg)
if err != nil {
    // handle error
}
```

---

## 🏗 Event Normalization Mapping

Each provider must map its internal events to these standard types:

| Standard Type        | Description                                        |
| :------------------- | :------------------------------------------------- |
| `thinking`           | AI is reasoning (e.g., Claude's `thinking` block). |
| `tool_use`           | AI is about to execute a local tool.               |
| `tool_result`        | The result of a tool execution.                    |
| `answer`             | Final or streaming text response.                  |
| `permission_request` | AI needs user approval for a sensitive action.     |
| `error`              | A provider-level or tool-level error.              |

---

## ⚙️ Configuration

Providers are configured via the `ProviderConfig` struct, which can be loaded from YAML/JSON:

```yaml
provider:
  type: "claude-code"
  enabled: true
  default_model: "claude-3-5-sonnet"
  allowed_tools: ["ls", "cat"]
  extra_args: ["--verbose"]
```

---

## 📁 File Structure

```
provider/
├── provider.go        # Core interfaces and types
├── plugin.go          # Plugin system (RFC #216)
├── factory.go         # Provider factory and registry
├── event.go           # Event types and normalization
├── permission.go      # Permission handling
├── claude_provider.go # Claude Code implementation
├── opencode_provider.go # OpenCode implementation
├── pi_provider.go     # Pi implementation
└── README.md          # This file
```

---

**Package Path**: `github.com/hrygo/hotplex/provider`
**Core Components**: `Provider`, `ProviderPlugin`, `ProviderFactory`, `ProviderEvent`
