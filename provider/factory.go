package provider

import (
	"fmt"
	"log/slog"
	"sync"
)

// ProviderFactory creates Provider instances based on configuration.
// It implements the Factory Pattern to decouple provider creation from usage.
type ProviderFactory struct {
	mu       sync.RWMutex
	creators map[ProviderType]ProviderCreator
	plugins  map[ProviderType]ProviderPlugin // Plugin metadata for introspection
	logger   *slog.Logger
}

// ProviderCreator is a function that creates a new Provider instance.
type ProviderCreator func(cfg ProviderConfig, logger *slog.Logger) (Provider, error)

// GlobalProviderFactory is the default factory instance.
// It comes pre-registered with built-in providers.
var GlobalProviderFactory *ProviderFactory
var once sync.Once

// InitGlobalProviderFactory initializes the global provider factory.
// This is called automatically on first use.
func InitGlobalProviderFactory() {
	once.Do(func() {
		GlobalProviderFactory = NewProviderFactory(slog.Default())
	})
}

// NewProviderFactory creates a new provider factory with default providers registered.
func NewProviderFactory(logger *slog.Logger) *ProviderFactory {
	if logger == nil {
		logger = slog.Default()
	}

	f := &ProviderFactory{
		creators: make(map[ProviderType]ProviderCreator),
		plugins:  make(map[ProviderType]ProviderPlugin),
		logger:   logger,
	}

	// Register built-in providers
	f.Register(ProviderTypeClaudeCode, func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
		return NewClaudeCodeProvider(cfg, logger)
	})

	f.Register(ProviderTypeOpenCode, func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
		return NewOpenCodeProvider(cfg, logger)
	})

	f.Register(ProviderTypePi, func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
		return NewPiProvider(cfg, logger)
	})

	return f
}

// Register adds a new provider creator to the factory.
// If a creator for the given type already exists, it will be replaced.
func (f *ProviderFactory) Register(t ProviderType, creator ProviderCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[t] = creator
	f.logger.Debug("Provider registered", "type", t)
}

// GetPlugin retrieves a registered plugin by type.
// Returns nil if the plugin is not registered.
func (f *ProviderFactory) GetPlugin(t ProviderType) ProviderPlugin {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.plugins[t]
}

// registerPlugin registers a provider plugin with the factory.
// This is an internal method called by RegisterPlugin.
func (f *ProviderFactory) registerPlugin(p ProviderPlugin) {
	f.mu.Lock()
	defer f.mu.Unlock()

	pt := p.Type()
	if pt == "" {
		f.logger.Warn("RegisterPlugin called with empty type")
		return
	}

	// Store plugin metadata
	f.plugins[pt] = p

	// Create creator wrapper that delegates to the plugin
	f.creators[pt] = func(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
		return p.New(cfg, logger)
	}

	f.logger.Info("Provider plugin registered", "type", pt)
}

// Create creates a new Provider instance based on the configuration.
func (f *ProviderFactory) Create(cfg ProviderConfig) (Provider, error) {
	f.mu.RLock()
	creator, ok := f.creators[cfg.Type]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", cfg.Type)
	}

	if cfg.Enabled != nil && !*cfg.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", cfg.Type)
	}

	provider, err := creator(cfg, f.logger)
	if err != nil {
		return nil, fmt.Errorf("create provider %s: %w", cfg.Type, err)
	}

	return provider, nil
}

// CreateDefault creates a Provider with default configuration.
func (f *ProviderFactory) CreateDefault(t ProviderType) (Provider, error) {
	enabled := true
	return f.Create(ProviderConfig{
		Type:    t,
		Enabled: &enabled,
	})
}

// BoolValue returns the value of a bool pointer if not nil, otherwise returns defaultVal.
func BoolValue(pb *bool, defaultVal bool) bool {
	if pb == nil {
		return defaultVal
	}
	return *pb
}

// ListRegistered returns a list of registered provider types.
func (f *ProviderFactory) ListRegistered() []ProviderType {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]ProviderType, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// IsRegistered checks if a provider type is registered.
func (f *ProviderFactory) IsRegistered(t ProviderType) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.creators[t]
	return ok
}

// CreateProvider is a convenience function that uses the global factory.
func CreateProvider(cfg ProviderConfig) (Provider, error) {
	InitGlobalProviderFactory()
	return GlobalProviderFactory.Create(cfg)
}

// CreateDefaultProvider is a convenience function for creating providers with defaults.
func CreateDefaultProvider(t ProviderType) (Provider, error) {
	InitGlobalProviderFactory()
	return GlobalProviderFactory.CreateDefault(t)
}

// ProviderRegistry maintains a cache of initialized providers.
// This is useful for reusing provider instances across sessions.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[ProviderType]Provider
	factory   *ProviderFactory
	logger    *slog.Logger
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry(factory *ProviderFactory, logger *slog.Logger) *ProviderRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProviderRegistry{
		providers: make(map[ProviderType]Provider),
		factory:   factory,
		logger:    logger,
	}
}

// Get retrieves a cached provider or creates a new one.
func (r *ProviderRegistry) Get(t ProviderType, cfg ProviderConfig) (Provider, error) {
	// First, try to get from cache
	r.mu.RLock()
	if provider, ok := r.providers[t]; ok {
		r.mu.RUnlock()
		return provider, nil
	}
	r.mu.RUnlock()

	// Create new provider
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double check after acquiring write lock
	if provider, ok := r.providers[t]; ok {
		return provider, nil
	}

	provider, err := r.factory.Create(cfg)
	if err != nil {
		return nil, err
	}

	r.providers[t] = provider
	r.logger.Info("Provider registered in cache", "type", t)

	return provider, nil
}

// GetOrCreate retrieves a cached provider or creates one with default config.
func (r *ProviderRegistry) GetOrCreate(t ProviderType) (Provider, error) {
	enabled := true
	return r.Get(t, ProviderConfig{
		Type:    t,
		Enabled: &enabled,
	})
}

// Remove removes a provider from the cache.
func (r *ProviderRegistry) Remove(t ProviderType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, t)
}

// Clear removes all providers from the cache.
func (r *ProviderRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = make(map[ProviderType]Provider)
}

// List returns all cached provider types.
func (r *ProviderRegistry) List() []ProviderType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]ProviderType, 0, len(r.providers))
	for t := range r.providers {
		types = append(types, t)
	}
	return types
}

// ValidateProviderConfig validates a provider configuration.
func ValidateProviderConfig(cfg ProviderConfig) error {
	if cfg.Type == "" {
		return fmt.Errorf("provider type is required")
	}

	// Validate type is known
	InitGlobalProviderFactory()
	if !GlobalProviderFactory.IsRegistered(cfg.Type) {
		return fmt.Errorf("unknown provider type: %s", cfg.Type)
	}

	return nil
}

// MergeProviderConfigs merges multiple provider configurations with precedence.
// Later configurations override earlier ones for non-zero values.
//
// Note: For boolean fields like Enabled, false cannot override true because
// false is the zero value. Use ExplicitDisable field in ProviderConfig if you
// need to explicitly disable a provider in an overlay config.
func MergeProviderConfigs(base, overlay ProviderConfig) ProviderConfig {
	result := base

	if overlay.Type != "" {
		result.Type = overlay.Type
	}
	// Use ExplicitDisable to override a true base.Enabled with false
	if overlay.ExplicitDisable {
		enabled := false
		result.Enabled = &enabled
	} else if overlay.Enabled != nil {
		result.Enabled = overlay.Enabled
	}
	if overlay.BinaryPath != "" {
		result.BinaryPath = overlay.BinaryPath
	}
	if overlay.DefaultModel != "" {
		result.DefaultModel = overlay.DefaultModel
	}
	if overlay.DefaultPermissionMode != "" {
		result.DefaultPermissionMode = overlay.DefaultPermissionMode
	}
	// DangerouslySkipPermissions: if overlay sets it to true, use that value
	if overlay.DangerouslySkipPermissions != nil && *overlay.DangerouslySkipPermissions {
		val := true
		result.DangerouslySkipPermissions = &val
	}
	if len(overlay.AllowedTools) > 0 {
		result.AllowedTools = overlay.AllowedTools
	}
	if len(overlay.DisallowedTools) > 0 {
		result.DisallowedTools = overlay.DisallowedTools
	}
	if len(overlay.ExtraArgs) > 0 {
		result.ExtraArgs = overlay.ExtraArgs
	}
	if len(overlay.ExtraEnv) > 0 {
		if result.ExtraEnv == nil {
			result.ExtraEnv = make(map[string]string)
		}
		for k, v := range overlay.ExtraEnv {
			result.ExtraEnv[k] = v
		}
	}
	if overlay.Timeout > 0 {
		result.Timeout = overlay.Timeout
	}
	if overlay.OpenCode != nil {
		result.OpenCode = overlay.OpenCode
	}
	if overlay.Pi != nil {
		result.Pi = overlay.Pi
	}

	return result
}
