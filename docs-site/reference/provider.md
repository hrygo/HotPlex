# HotPlex Provider Reference

The `provider` package defines the bridge between HotPlex and various AI CLI agents (e.g., Claude Code, OpenCode). It abstracts platform-specific CLI protocols, event formats, and execution models into a unified interface.

## 🏛 Architecture Overview

The Providers act as **Strategy Adapters** in the HotPlex ecosystem. They handle the low-level details of interacting with different AI agents while exposing a consistent API to the Engine.

```mermaid
graph TD
    Engine[HotPlex Engine] --> Factory[Provider Factory]
    Factory --> Claude[Claude Code Provider]
    Factory --> OpenCode[OpenCode Provider]
    Factory --> Custom[Custom Provider]

    subgraph Interface [Unified Interface]
        Provider[Provider Interface]
        Event[Normalized Event Model]
    end

    Claude -.-> Provider
    OpenCode -.-> Provider
    Custom -.-> Provider
```

### Key Architectural Concepts

-   **`Provider` (Interface)**: The core contract that defines how to start a CLI, send user input, and parse the resulting stream of events.
-   **Normalized Event Model**: Converts provider output into a standard `ProviderEvent` stream (e.g., `thinking`, `tool_use`, `answer`, `error`).
-   **Factory Pattern**: The `ProviderFactory` allows for dynamic registration and creation of providers based on configuration.
-   **Protocol Translation**: Handles the specific "dialect" of each underlying CLI (e.g., Claude's `stream-json` vs. OpenCode's reasoning parts).

---

## 🛠 Developer Guide

### 1. Implementing a New Provider

To support a new AI CLI tool, implement the `Provider` interface:

```go
type MyNewProvider struct {
    provider.ProviderBase // Optional helper
}

func (p *MyNewProvider) Name() string { return "my-new-ai" }

func (p *MyNewProvider) BuildCLIArgs(sessionID string, opts *ProviderSessionOptions) []string {
    // Construct command line arguments
}

func (p *MyNewProvider) BuildInputMessage(prompt string, taskInst string) (map[string]any, error) {
    // Format the stdin payload for the CLI
}

func (p *MyNewProvider) ParseEvent(line string) ([]*ProviderEvent, error) {
    // Convert a raw line of stdout to normalized events
}
```

### 2. Registering with the Factory

In your package's `init()` or an initialization function, register your provider with the global factory:

```go
provider.GlobalProviderFactory.Register("my-new-ai", func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
    return &MyNewProvider{...}, nil
})
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

Providers are configured via the `ProviderConfig` struct:

```yaml
provider:
  type: "claude-code"
  enabled: true
  default_model: "claude-3-5-sonnet"
  allowed_tools: ["ls", "cat"]
  extra_args: ["--verbose"]
```
