package sinks

import (
	"fmt"
	"log/slog"
	"sync"
)

// Factory creates a Sink from a config map.
type Factory func(config map[string]any, log *slog.Logger) (Sink, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{
		"noop": func(_ map[string]any, _ *slog.Logger) (Sink, error) {
			return NewNoopSink(), nil
		},
		"log": func(_ map[string]any, log *slog.Logger) (Sink, error) {
			return NewLogSink(log), nil
		},
	}
)

// Register adds a new sink factory. P1: in-code only; external RegisterSink
// (from spec section 5.6) is deferred to P2.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Build constructs a sink by name. Returns error if name is not registered.
func Build(name string, config map[string]any, log *slog.Logger) (Sink, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("audit sinks: unknown sink name %q", name)
	}
	return factory(config, log)
}
