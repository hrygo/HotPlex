package messaging

import (
	"fmt"
	"log/slog"
	"sync"
)

// AdapterBuilder creates a new adapter instance.
type AdapterBuilder func(log *slog.Logger) PlatformAdapterInterface

var (
	registryMu sync.RWMutex
	registry   = make(map[PlatformType]AdapterBuilder)
)

// Register records an adapter builder under its platform type.
func Register(pt PlatformType, builder AdapterBuilder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if builder == nil {
		panic(fmt.Sprintf("messaging: nil builder for platform %q", pt))
	}
	if _, exists := registry[pt]; exists {
		panic(fmt.Sprintf("messaging: duplicate registration for platform %q", pt))
	}
	registry[pt] = builder
}

// New creates an adapter by type.
func New(pt PlatformType, log *slog.Logger) (PlatformAdapterInterface, error) {
	registryMu.RLock()
	b, ok := registry[pt]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("messaging: unknown platform %q", pt)
	}
	return b(log.With("platform", string(pt))), nil
}

// RegisteredTypes returns all registered platform types.
func RegisteredTypes() []PlatformType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]PlatformType, 0, len(registry))
	for pt := range registry {
		types = append(types, pt)
	}
	return types
}
