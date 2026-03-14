# 核心引擎内存与 CPU 优化设计

**Date**: 2026-03-14
**Status**: Draft
**Target**: Core Engine Performance Optimization

---

## 1. 背景与目标

当前 HotPlex 核心引擎 (`internal/engine/`) 在内存分配和 CPU 使用方面存在优化空间：

- Scanner 预分配 256KB buffer，小输出场景浪费严重
- 字符串拼接使用 `fmt.Sprintf` 引入不必要开销
- Cleanup 循环使用固定 1 分钟间隔，无视 timeout 配置

**目标**：减少内存浪费、降低 CPU 开销，同时保持功能不变。

---

## 2. 优化方案

### 2.1 Scanner Buffer 动态缩放

**问题**：`types.go:23` 固定初始 buffer 256KB，无论实际输出多少都预分配大内存。

**方案**：从 4KB 开始，按需倍增。

```go
// internal/engine/types.go
const (
    ScannerInitialBufSize = 4 * 1024      // 4 KB 起步
    ScannerMaxBufSize     = 512 * 1024    // 512 KB 大多数场景够用
)
```

**影响**：
- 小输出场景内存减少 98% (256KB → 4KB)
- 大输出场景自动扩容至 512KB，足够 99% 场景

---

### 2.2 字符串拼接优化

**问题**：`pool.go:236` 每次创建 session 使用 `fmt.Sprintf`：
```go
uniqueStr := fmt.Sprintf("%s:session:%s", sm.opts.Namespace, sessionID)
```

**方案**：直接使用 `+` 拼接（Go 编译器对短字符串有优化）。

```go
// internal/engine/pool.go

// 替换 fmt.Sprintf
uniqueStr := sm.opts.Namespace + ":session:" + sessionID
```

**影响**：
- 消除反射开销
- Go 编译器对短字符串拼接有内置优化

---

### 2.3 Cleanup Loop 自适应触发

**问题**：`types.go:30` 固定 1 分钟检查间隔，无视 timeout 配置。

**方案**：基于 timeout 动态计算间隔。

```go
// internal/engine/pool.go
func (sm *SessionPool) cleanupInterval() time.Duration {
    // 间隔 = timeout / 4，限制在 [1min, 5min]
    interval := sm.timeout / 4
    if interval > 5*time.Minute {
        interval = 5 * time.Minute
    }
    if interval < 1*time.Minute {
        interval = 1 * time.Minute
    }
    return interval
}
```

**影响**：
- 减少无效遍历（timeout=30min 时，间隔从 1min 变为 7.5min）
- CPU 使用更平滑

---

## 3. 修改清单

| 文件 | 修改内容 |
|------|----------|
| `internal/engine/types.go` | 修改 `ScannerInitialBufSize`, `ScannerMaxBufSize` 常量，删除 `CleanupCheckInterval` |
| `internal/engine/pool.go` | 添加 `cleanupInterval()` 方法，优化字符串拼接 |

---

## 4. 测试计划

- [ ] 单元测试：`ScannerInitialBufSize` 修改后 scanner 行为正常
- [ ] 集成测试：创建/销毁 session 功能正常
- [ ] 性能验证：对比优化前后内存分配（可选）

---

## 5. 风险与回滚

- **风险**：Scanner buffer 缩小可能导致大输出被截断
  - **缓解**：保留 512KB 上限，足够大多数 CLI 输出
- **回滚**：修改常量值即可恢复原状

---

## 6. 里程碑

1. 修改 `types.go` 常量
2. 修改 `pool.go` 字符串拼接
3. 添加 `cleanupInterval()` 方法
4. 运行 `go test ./internal/engine/...`
5. 提交代码
