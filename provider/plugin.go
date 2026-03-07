package provider

import (
	"fmt"
	"log/slog"
	"sync"
)

// ProviderPlugin defines the interface for provider plugins.
// Third-party extensions implement this interface to register custom providers
// without modifying HotPlex core code.
//
// Usage:
//
//	// external/aider/provider.go
//	type aiderPlugin struct{}
//
//	func (p *aiderPlugin) Type() ProviderType { return "aider" }
//	func (p *aiderPlugin) New(cfg ProviderConfig, logger *slog.Logger) (Provider, error) { ... }
//	func (p *aiderPlugin) Meta() ProviderMeta { ... }
//
//	func init() {
//	    provider.RegisterPlugin(&aiderPlugin{})
//	}
type ProviderPlugin interface {
	// Type returns the unique provider type identifier.
	Type() ProviderType

	// New creates a new Provider instance with the given configuration.
	// The logger may be nil, in which case the default logger should be used.
	New(cfg ProviderConfig, logger *slog.Logger) (Provider, error)

	// Meta returns the provider's metadata including capabilities.
	Meta() ProviderMeta
}

// pluginRegistry holds registered provider plugins.
type pluginRegistry struct {
	mu      sync.RWMutex
	plugins map[ProviderType]ProviderPlugin
}

// globalPluginRegistry is the singleton plugin registry.
var globalPluginRegistry = &pluginRegistry{
	plugins: make(map[ProviderType]ProviderPlugin),
}

// RegisterPlugin registers a provider plugin with the global registry.
// This is typically called from an init() function in the plugin package.
//
// Example:
//
//	func init() {
//	    provider.RegisterPlugin(&myCustomPlugin{})
//	}
//
// Plugins registered here are automatically available through the
// GlobalProviderFactory after initialization.
func RegisterPlugin(p ProviderPlugin) {
	if p == nil {
		panic("provider: cannot register nil plugin")
	}

	pt := p.Type()
	if pt == "" {
		panic("provider: plugin has empty type")
	}

	// Validate plugin metadata
	if err := validatePlugin(p); err != nil {
		panic(fmt.Sprintf("provider: %v", err))
	}

	globalPluginRegistry.mu.Lock()
	defer globalPluginRegistry.mu.Unlock()

	// Check for duplicate registration
	if _, exists := globalPluginRegistry.plugins[pt]; exists {
		// Allow re-registration for testing and hot-reload scenarios
		slog.Debug("Provider plugin re-registered", "type", pt)
	}

	globalPluginRegistry.plugins[pt] = p
	slog.Debug("Provider plugin registered", "type", pt)

	// Also register with the global factory if it's already initialized
	if GlobalProviderFactory != nil {
		GlobalProviderFactory.registerPlugin(p)
	}
}

// GetPlugin retrieves a registered plugin by type.
// Returns nil if the plugin is not registered.
func GetPlugin(t ProviderType) ProviderPlugin {
	globalPluginRegistry.mu.RLock()
	defer globalPluginRegistry.mu.RUnlock()
	return globalPluginRegistry.plugins[t]
}

// ListPlugins returns all registered plugin types.
func ListPlugins() []ProviderType {
	globalPluginRegistry.mu.RLock()
	defer globalPluginRegistry.mu.RUnlock()

	types := make([]ProviderType, 0, len(globalPluginRegistry.plugins))
	for t := range globalPluginRegistry.plugins {
		types = append(types, t)
	}
	return types
}

// IsPluginRegistered checks if a plugin type is registered.
func IsPluginRegistered(t ProviderType) bool {
	globalPluginRegistry.mu.RLock()
	defer globalPluginRegistry.mu.RUnlock()
	_, ok := globalPluginRegistry.plugins[t]
	return ok
}

// PluginMetadataError is returned when plugin metadata validation fails.
type PluginMetadataError struct {
	Type    ProviderType
	Message string
}

func (e *PluginMetadataError) Error() string {
	return fmt.Sprintf("plugin %q metadata error: %s", e.Type, e.Message)
}

// validatePlugin performs basic validation on a plugin.
func validatePlugin(p ProviderPlugin) error {
	meta := p.Meta()
	if meta.Type == "" {
		return &PluginMetadataError{
			Type:    p.Type(),
			Message: "metadata has empty type",
		}
	}
	if meta.Type != p.Type() {
		return &PluginMetadataError{
			Type:    p.Type(),
			Message: fmt.Sprintf("metadata type %q does not match plugin type %q", meta.Type, p.Type()),
		}
	}
	if meta.DisplayName == "" {
		return &PluginMetadataError{
			Type:    p.Type(),
			Message: "metadata has empty display name",
		}
	}
	if meta.BinaryName == "" {
		return &PluginMetadataError{
			Type:    p.Type(),
			Message: "metadata has empty binary name",
		}
	}
	return nil
}
