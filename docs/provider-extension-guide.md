# Provider Extension Guide

This guide explains how to extend HotPlex with custom AI CLI providers using the plugin system.

## Overview

HotPlex supports a plugin-based architecture for providers. This allows third-party developers to add new AI CLI providers without modifying the core codebase.

## Architecture

```
┌─────────────────────────────────────────┐
│          ProviderPlugin              │
│   (Interface in provider/plugin.go)   │
└───────────────────┬───────────────┘
                    │
    ┌─────────────────────────────────────┐
                    │
          ┌─────────▼──────────┐
          │                   │
┌──────────────▼─────────────▼──────────────┐
│   globalPluginRegistry    │   ProviderFactory    │
│   (Plugin Storage)        │   (Creator Storage)  │
└──────────────────────────┘──────────────────────────┘
```

## Creating a Custom Provider

### Step 1: Implement the ProviderPlugin Interface

```go
// myprovider/provider.go
package myprovider

import (
    "fmt"
    "log/slog"

    "github.com/hrygo/hotplex/provider"
)

// myPlugin implements the ProviderPlugin interface.
type myPlugin struct{}

// Type returns the unique provider type identifier.
func (p *myPlugin) Type() provider.ProviderType {
    return "my-provider"
}

// New creates a new provider instance.
func (p *myPlugin) New(cfg provider.ProviderConfig, logger *slog.Logger) (provider.Provider, error) {
    if logger == nil {
        logger = slog.Default()
    }
    // Your provider initialization logic here
    return &myProviderImpl{cfg: cfg, logger: logger}, nil
}

// Meta returns provider metadata.
func (p *myPlugin) Meta() provider.ProviderMeta {
    return provider.ProviderMeta{
        Type:        "my-provider",
        DisplayName: "My Custom Provider",
        BinaryName:  "my-cli",
        Version:     "1.0.0",
        Features: provider.ProviderFeatures{
            SupportsResume:     true,
            SupportsStreamJSON: true,
            SupportsSSE:        false,
            SupportsHTTPAPI:   false,
            SupportsSessionID:  true,
            SupportsPermissions: true,
            MultiTurnReady:     true,
        },
    }
}
```

### Step 2: Implement the Provider Interface

Your provider must implement the `provider.Provider` interface:

```go
// myProviderImpl implements the Provider interface.
type myProviderImpl struct {
    cfg    provider.ProviderConfig
    logger *slog.Logger
}

func (p *myProviderImpl) Metadata() provider.ProviderMeta {
    return provider.ProviderMeta{
        Type:        "my-provider",
        DisplayName: "My Custom Provider",
        BinaryName:  "my-cli",
    }
}

func (p *myProviderImpl) BuildCLIArgs(sessionID string, opts *provider.ProviderSessionOptions) []string {
    // Build CLI arguments
    return []string{"--session-id", sessionID}
}

func (p *myProviderImpl) BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error) {
    return map[string]any{
        "prompt": prompt,
        "task_instructions": taskInstructions,
    }, nil
}

func (p *myProviderImpl) ParseEvent(line string) ([]*provider.ProviderEvent, error) {
    // Parse CLI output into events
    return nil, nil
}

func (p *myProviderImpl) DetectTurnEnd(event *provider.ProviderEvent) bool {
    return event.Type == provider.EventTypeResult
}

func (p *myProviderImpl) ValidateBinary() (string, error) {
    return exec.LookPath("my-cli")
}

func (p *myProviderImpl) CleanupSession(sessionID string, workDir string) error {
    return nil
}

func (p *myProviderImpl) Name() string {
    return "my-provider"
}
```

### Step 3: Register the Plugin

Use `init()` to auto-register your plugin when the package is imported:

```go
// myprovider/provider.go
func init() {
    provider.RegisterPlugin(&myPlugin{})
}
```

### Step 4: Import Your Plugin

Import your plugin package in your application startup:

```go
import _ "github.com/yourorg/myprovider"

func main() {
    // Plugin is automatically registered via init()
    // Use the provider
    cfg := provider.ProviderConfig{
        Type:    "my-provider",
        Enabled: true,
    }

    p, err := provider.CreateProvider(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Use the provider...
}
```

## Plugin Registration Flow

1. Package `init()` calls `provider.RegisterPlugin()`
2. Plugin is stored in `globalPluginRegistry`
3. If `GlobalProviderFactory` exists, plugin is also registered with factory
4. Provider can be created via `provider.CreateProvider()`

## Metadata Requirements

Provider metadata must include:

| Field | Description | Required |
|-------|-------------|----------|
| Type | Unique identifier | Yes |
| DisplayName | Human-readable name | Yes |
| BinaryName | CLI binary name | Yes |
| Version | Provider version | No |
| Features | Capability flags | Yes |

## Feature Flags

| Flag | Description |
|------|-------------|
| SupportsResume | Can resume sessions |
| SupportsStreamJSON | Supports JSON streaming |
| SupportsSSE | Supports Server-Sent Events |
| SupportsHTTPAPI | Has HTTP API mode |
| SupportsSessionID | Supports session IDs |
| SupportsPermissions | Supports permission modes |
| MultiTurnReady | Multi-turn capable |

## Best Practices
1. Use unique type identifiers to avoid conflicts
2. Validate binary path in `ValidateBinary()`
3. Handle errors gracefully in `New()`
4. Log important events
5. Clean up resources in `CleanupSession()`

## Testing Your Plugin

```go
func TestMyPlugin(t *testing.T) {
    // Register plugin
    p := &myPlugin{}
    provider.RegisterPlugin(p)

    // Verify registration
    if !provider.IsPluginRegistered("my-provider") {
        t.Fatal("Plugin not registered")
    }

    // Create provider
    cfg := provider.ProviderConfig{
        Type:    "my-provider",
        Enabled: true,
    }

    prov, err := provider.CreateProvider(cfg)
    if err != nil {
        t.Fatalf("CreateProvider failed: %v", err)
    }

    // Verify provider works
    if prov.Name() != "my-provider" {
        t.Fatalf("Unexpected provider name: %s", prov.Name())
    }
}
```

## Migration from Built-in Providers

Built-in providers (claude-code, opencode, pi) are automatically registered and `GlobalProviderFactory`. Custom plugins follow the same pattern and are coexist with built-in providers.

To check all registered providers:
```go
types := provider.GlobalProviderFactory.ListRegistered()
// ["claude-code", "opencode", "pi", "my-provider"]
```
