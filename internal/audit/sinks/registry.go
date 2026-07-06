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

// Register adds a new sink factory. It is the public extension point for
// third-party code (spec §5.6, issue #833 P2). Call from a package init() so
// registration happens before the collector builds sinks at gateway startup.
//
// Panics on nil factory. A re-registration under an existing name overwrites
// the prior factory (idempotent; safe under go test -count=N within one
// binary, and lets a late-loading package override a built-in). This is a
// deliberate softening of the worker.Register hard-panic convention: sinks
// are config-driven and user-replaceable, so a conflict is a config choice,
// not a bug. Sink names should be lowercase snake_case and SHOULD be prefixed
// with the registering package/import path to avoid accidental collisions
// (e.g. "mycorp_slack").
func Register(name string, factory Factory) {
	if factory == nil {
		panic("audit sinks: register factory is nil for " + name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Registered returns the names of all registered sink factories, for
// diagnostics and config validation ("did you mean ...?" error messages).
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
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
