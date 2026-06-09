package worker

import (
	"fmt"
	"sync"
)

// ─── Registry ───────────────────────────────────────────────────────────────

// Builder creates a Worker instance.
type Builder func() (Worker, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[WorkerType]Builder)

	// Capability cache: populated once at Register() time to avoid
	// creating temporary worker instances for capability queries.
	capCache   = make(map[WorkerType]bool)
	capCacheMu sync.RWMutex
)

// Register registers a new worker builder for the given worker type.
// It panics if the builder is nil or if a type is registered twice.
//
// Register eagerly invokes b() once to cache CanResumeTerminated capability;
// builders with expensive initialization should ensure construction is lightweight.
func Register(t WorkerType, b Builder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if b == nil {
		panic("worker: register builder is nil")
	}
	if _, dup := registry[t]; dup {
		panic("worker: register called twice for type " + string(t))
	}
	registry[t] = b

	// Eagerly cache CanResumeTerminated by creating one temporary instance.
	// Builder error (e.g. CodexCLI GetSingleton not ready) implies cannot resume.
	if w, err := b(); err == nil {
		capCacheMu.Lock()
		capCache[t] = w != nil && w.CanResumeTerminated()
		capCacheMu.Unlock()
	} else {
		capCacheMu.Lock()
		capCache[t] = false
		capCacheMu.Unlock()
	}
}

// NewWorker creates a new Worker instance for the specified worker type.
// It returns an error if the type is unknown or if the builder fails.
func NewWorker(t WorkerType) (Worker, error) {
	registryMu.RLock()
	b, ok := registry[t]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("worker: unknown type %q", t)
	}
	return b()
}

// RegisteredTypes returns a list of all registered worker types.
func RegisteredTypes() []WorkerType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var types []WorkerType
	for t := range registry {
		types = append(types, t)
	}
	return types
}

// CanResumeTerminated returns true if the given worker type supports
// resuming sessions in TERMINATED state (orphan recovery).
// Uses register-time capability cache — no temporary worker allocation.
func CanResumeTerminated(t WorkerType) bool {
	capCacheMu.RLock()
	v, ok := capCache[t]
	capCacheMu.RUnlock()
	if ok {
		return v
	}
	// Fallback for types registered before capability caching existed.
	w, err := NewWorker(t)
	if err != nil || w == nil {
		return false
	}
	return w.CanResumeTerminated()
}
