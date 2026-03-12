package apphome

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry manages capability definitions with thread-safe access.
type Registry struct {
	mu           sync.RWMutex
	capabilities map[string]Capability
	categories   []CategoryInfo
	configPath   string
	logger       *slog.Logger
}

// RegistryOption configures a Registry.
type RegistryOption func(*Registry)

// WithLogger sets the logger for the registry.
func WithLogger(logger *slog.Logger) RegistryOption {
	return func(r *Registry) {
		r.logger = logger
	}
}

// WithConfigPath sets the path to the capabilities config file.
func WithConfigPath(path string) RegistryOption {
	return func(r *Registry) {
		r.configPath = path
	}
}

// WithCategories sets custom categories.
func WithCategories(categories []CategoryInfo) RegistryOption {
	return func(r *Registry) {
		r.categories = categories
	}
}

// NewRegistry creates a new capability registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		capabilities: make(map[string]Capability),
		categories:   DefaultCategories(),
		logger:       slog.Default(),
		configPath:   "",
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Load loads capabilities from the configured YAML file.
func (r *Registry) Load() error {
	if r.configPath == "" {
		r.logger.Debug("No config path specified, skipping load")
		return nil
	}

	data, err := os.ReadFile(r.configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var cfg struct {
		Capabilities []Capability `yaml:"capabilities"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing capabilities
	r.capabilities = make(map[string]Capability)

	// Load and validate each capability
	for _, cap := range cfg.Capabilities {
		if err := cap.Validate(); err != nil {
			r.logger.Warn("Invalid capability, skipping",
				"capability_id", cap.ID,
				"error", err)
			continue
		}

		// Skip disabled capabilities
		if !cap.Enabled {
			r.logger.Debug("Capability disabled, skipping",
				"capability_id", cap.ID)
			continue
		}

		r.capabilities[cap.ID] = cap
		r.logger.Debug("Loaded capability",
			"capability_id", cap.ID,
			"category", cap.Category)
	}

	r.logger.Info("Capabilities loaded", "count", len(r.capabilities))
	return nil
}

// Reload reloads capabilities from the config file.
func (r *Registry) Reload() error {
	r.logger.Info("Reloading capabilities")
	return r.Load()
}

// Get retrieves a capability by ID.
func (r *Registry) Get(id string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cap, ok := r.capabilities[id]
	return cap, ok
}

// GetAll returns all registered capabilities.
func (r *Registry) GetAll() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Capability, 0, len(r.capabilities))
	for _, cap := range r.capabilities {
		result = append(result, cap)
	}
	return result
}

// GetByCategory returns capabilities filtered by category.
func (r *Registry) GetByCategory(category string) []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Capability
	for _, cap := range r.capabilities {
		if cap.Category == category {
			result = append(result, cap)
		}
	}
	return result
}

// GetCategories returns all available categories.
func (r *Registry) GetCategories() []CategoryInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]CategoryInfo, len(r.categories))
	copy(result, r.categories)
	return result
}

// Register adds a capability to the registry.
func (r *Registry) Register(cap Capability) error {
	if err := cap.Validate(); err != nil {
		return fmt.Errorf("validate capability: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.capabilities[cap.ID] = cap
	r.logger.Debug("Registered capability", "capability_id", cap.ID)
	return nil
}

// Unregister removes a capability from the registry.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.capabilities, id)
	r.logger.Debug("Unregistered capability", "capability_id", id)
}

// ConfigPath returns the current config file path.
func (r *Registry) ConfigPath() string {
	return r.configPath
}

// Count returns the number of registered capabilities.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.capabilities)
}

// LoadFromBytes loads capabilities from YAML data.
// Useful for embedding default capabilities or testing.
func (r *Registry) LoadFromBytes(data []byte) error {
	var cfg struct {
		Capabilities []Capability `yaml:"capabilities"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cap := range cfg.Capabilities {
		if err := cap.Validate(); err != nil {
			r.logger.Warn("Invalid capability, skipping",
				"capability_id", cap.ID,
				"error", err)
			continue
		}

		if !cap.Enabled {
			continue
		}

		r.capabilities[cap.ID] = cap
	}

	r.logger.Info("Capabilities loaded from bytes", "count", len(r.capabilities))
	return nil
}
