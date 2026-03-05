package storage

import "sync"

// PluginConfig 插件配置
type PluginConfig map[string]any

// PluginFactory 插件工厂接口
type PluginFactory interface {
	Create(config PluginConfig) (ChatAppMessageStore, error)
}

// PluginRegistry 插件注册表
type PluginRegistry struct {
	mu        sync.RWMutex
	factories map[string]PluginFactory
}

var globalRegistry *PluginRegistry
var registryOnce sync.Once

func GlobalRegistry() *PluginRegistry {
	registryOnce.Do(func() {
		globalRegistry = NewPluginRegistry()
	})
	return globalRegistry
}

func NewPluginRegistry() *PluginRegistry {
	r := &PluginRegistry{factories: make(map[string]PluginFactory)}
	r.Register("memory", &MemoryFactory{})
	r.Register("sqlite", &SQLiteFactory{})
	r.Register("postgresql", &PostgreFactory{})
	return r
}

func (r *PluginRegistry) Register(name string, factory PluginFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

func (r *PluginRegistry) Get(name string, config PluginConfig) (ChatAppMessageStore, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[name]
	if !ok {
		return nil, nil
	}
	return factory.Create(config)
}

func (r *PluginRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}
