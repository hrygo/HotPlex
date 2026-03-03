# HotPlex Cache Layer

可插拔缓存层，支持多种后端实现，默认为 No-Op（无操作）以保持向后兼容。

## 设计目标

1. **向后兼容**: 默认 No-Op 实现，不影响现有功能
2. **可扩展性**: 清晰的接口设计，易于添加新后端
3. **统一 API**: 所有后端使用相同的接口
4. **类型安全**: 完整的类型定义和错误处理

## 架构

```
┌─────────────────────────────────────────┐
│           Application Layer             │
├─────────────────────────────────────────┤
│            Cache Helper                 │
│  (GetJSON, SetJSON, GetOrCompute)       │
├─────────────────────────────────────────┤
│           Cache Interface               │
│  (Get, Set, Delete, Exists, Clear)      │
├─────────────────────────────────────────┤
│         Backend Implementations         │
│  ┌─────────┬─────────┬─────────────┐   │
│  │ NoOp    │ Memory  │ Redis       │   │
│  │ (Default)│ (TODO)  │ (TODO)      │   │
│  └─────────┴─────────┴─────────────┘   │
└─────────────────────────────────────────┘
```

## 核心接口

### Cache

所有缓存后端必须实现的基本接口：

```go
type Cache interface {
    Get(ctx context.Context, key string) (*CacheEntry, error)
    Set(ctx context.Context, key string, value []byte, opts ...CacheOption) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error
    Close() error
    Name() string
}
```

### TaggedCache

支持标签操作的扩展接口：

```go
type TaggedCache interface {
    Cache
    DeleteByTag(ctx context.Context, tag string) error
    ListKeysByTag(ctx context.Context, tag string) ([]string, error)
}
```

### StatsProvider

提供统计信息的接口：

```go
type StatsProvider interface {
    GetStats(ctx context.Context) (*CacheStats, error)
}
```

## 使用示例

### 基础使用

```go
import "github.com/hrygo/hotplex/cache"

// 使用全局缓存（默认 No-Op）
ctx := context.Background()
err := cache.Set(ctx, "key", []byte("value"), cache.WithTTL(1*time.Hour))
entry, err := cache.Get(ctx, "key")
```

### JSON 数据

```go
type MyData struct {
    Name  string
    Value int
}

data := &MyData{Name: "test", Value: 42}

// 存储 JSON
err := cache.SetJSON(ctx, "my-key", data, cache.WithTTL(cache.TTLMedium))

// 读取 JSON
var result MyData
err := cache.GetJSON(ctx, "my-key", &result)
```

### GetOrCompute 模式

```go
value, err := cache.GetOrCompute(ctx, "expensive-key", 
    func() ([]byte, error) {
        // 执行昂贵操作
        return computeExpensiveResult()
    },
    cache.WithTTL(cache.TTLLong),
)
```

### 自定义缓存后端

```go
// 1. 实现 Cache 接口
type MyCustomCache struct {
    // 后端特定字段
}

func (c *MyCustomCache) Get(ctx context.Context, key string) (*CacheEntry, error) {
    // 实现获取逻辑
}

func (c *MyCustomCache) Set(ctx context.Context, key string, value []byte, opts ...CacheOption) error {
    // 实现设置逻辑
}

// ... 实现其他方法

// 2. 注册为全局缓存
cache.SetGlobalCache(&MyCustomCache{})
```

### 使用 CacheHelper

```go
helper := cache.NewCacheHelper(myCache)

// JSON 操作
err := helper.SetJSON(ctx, "key", data, cache.WithTTL(1*time.Hour))
err := helper.GetJSON(ctx, "key", &result)

// GetOrCompute
data, err := helper.GetOrCompute(ctx, "key", computeFunc, opts...)
```

## 缓存键 helpers

```go
// Prompt 缓存
key := cache.PromptCacheKey(sessionID, prompt)

// Response 缓存
key := cache.ResponseCacheKey(sessionID, prompt, model)

// Session 上下文缓存
key := cache.SessionCacheKey(sessionID)

// Tool 结果缓存
key := cache.ToolCacheKey("bash", args)
```

## TTL 预设

```go
cache.TTLShort      // 5 分钟
cache.TTLMedium     // 1 小时
cache.TTLLong       // 24 小时
cache.TTLPermanent  // 永久（无过期）
```

## 统计信息

```go
stats, err := cache.GetGlobalCache().(cache.StatsProvider).GetStats(ctx)
fmt.Printf("Hit Ratio: %.2f%%\n", stats.HitRatio() * 100)
fmt.Printf("Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
```

## 未来扩展

### 计划实现的后端

1. **MemoryCache**: 进程内 LRU 缓存
2. **RedisCache**: Redis 后端，支持分布式缓存
3. **SemanticCache**: 基于向量相似度的语义缓存

### 扩展点

- 实现 `Cache` 接口添加新后端
- 实现 `TaggedCache` 接口添加标签支持
- 实现 `StatsProvider` 接口添加统计功能

## 注意事项

1. **线程安全**: 所有实现必须保证并发安全
2. **错误处理**: 返回明确的错误类型（如 `ErrCacheMiss`）
3. **上下文**: 所有操作支持 `context.Context` 用于取消和超时
4. **序列化**: JSON 辅助函数使用 `encoding/json`，自定义后端可优化

## 测试

```bash
go test ./cache/... -v
```

## 性能考虑

- No-Op 实现零开销
- Memory 缓存应避免大对象
- Redis 缓存注意网络连接池
- 合理设置 TTL 避免内存泄漏
