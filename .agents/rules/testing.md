---
paths:
  - "**/*_test.go"
  - "**/testutil/**/*.go"
---

# 测试规范

> 断言库 / table-driven / race 检测规范 → 见 AGENTS.md 约定与规范

## 全局单例并发隔离（OCS Worker）

涉及 `atomic.Pointer` 全局单例（如 OCS `SingletonProcessManager`）的测试必须防止并发状态污染：

```go
t.Run("acquire then release", func(t *testing.T) {
    ResetSingletonForTest()  // 将 atomic.Pointer 置为 nil
    defer CleanSingletonForTest()

    sm := GetSingleton()
    err := sm.Acquire(context.Background())
    require.NoError(t, err)
    sm.Release()
})
```

**并发模式下禁用**：`if testing.Short() { t.Skip("skipping singleton test in short mode") }`

**禁止**：
- 在 `t.Parallel()` 子测试中共享同一单例引用（除非测试的是单例自身行为）
- 跨测试修改全局单例后不还原，导致后续测试被污染

## 资源清理

```go
// 使用 t.Cleanup() 确保资源释放
db, err := sql.Open("sqlite", ":memory:")
require.NoError(t, err)
t.Cleanup(func() { db.Close() })

// 临时目录
dir := t.TempDir()  // 自动清理，无需 t.Cleanup
```

## 测试性能要求

- **单模块 ≤5s**：`go test ./<pkg>/... -count=1 -race` 任一模块不得超过 5 秒
- **避免 `time.Sleep`**：用 `require.Eventually`、channel 信号、context 超时或 mock 回调替代固定等待；唯一例外是测试定时器/退避逻辑时可用极短 sleep（≤30ms）
- **集成测试隔离**：需要外部二进制或网络服务的测试必须用 `testing.Short()` guard，CI 以 `-short` 运行时自动 skip
- **`t.Parallel()` 优先**：无共享状态的测试一律加 `t.Parallel()`，充分利用多核

### 反模式（禁止）

- ❌ `time.Sleep(100 * time.Millisecond)` 等待异步结果（改用 `require.Eventually`）
- ❌ 无超时的 channel 阻塞（必须 `select` + `time.After` 或 context）
- ❌ 启动真实进程的测试不加 `testing.Short()` guard

## 测试工具

```
internal/gateway/testutil/  — WebSocket mock helpers（MockConn, WriteEnvelope, ReadEnvelope）
internal/messaging/mock/    — Mock messaging adapter（bridge/handler 集成测试）
```

## E2E 测试

- `e2e/` 目录：端到端集成测试
- 需要 gateway 运行的测试用 `// +build e2e` 或短 flag 跳过
