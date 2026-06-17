package worker

import (
	"errors"
	"fmt"
	"slices"
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
	// NOTE: This is a register-time snapshot; it does not reflect runtime state
	// changes. Workers whose CanResumeTerminated depends on runtime conditions
	// should return the base value here and handle runtime checks at call sites.
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

// ErrInvalidWorkerType signals a worker type that is not registered.
// Returned by ValidateType for non-empty types absent from RegisteredTypes().
var ErrInvalidWorkerType = errors.New("invalid worker type")

// ValidateType returns nil for an empty wt (means "inherit default") or for any
// registered worker type. A non-empty unregistered type returns ErrInvalidWorkerType.
// Used by both the PATCH workspace handler and CreateSession to reject unknown
// worker types at the boundary instead of at worker launch. See spec ③ §4.
func ValidateType(wt WorkerType) error {
	if wt == "" {
		return nil
	}
	registered := RegisteredTypes()
	if slices.Contains(registered, wt) {
		return nil
	}
	return fmt.Errorf("%w: %q not in registered types %v", ErrInvalidWorkerType, wt, registered)
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
	// Fallback: should not be reached in normal operation (all types cached at
	// Register time). Returns false rather than allocating a temporary worker,
	// since builders may acquire resources (processes, connections).
	return false
}
