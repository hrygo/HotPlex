# 核心引擎内存与 CPU 优化实现计划

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 减少 Scanner buffer 预分配、优化字符串拼接、自适应 cleanup 间隔

**Architecture:** 修改 engine 层的常量配置和方法，无需新增文件

**Tech Stack:** Go 1.25, sync, strings

---

## 准备

- [ ] 创建新分支: `git checkout -b feat/engine-memory-cpu-optimization`

---

## Chunk 1: Scanner Buffer 动态缩放

### Task 1: 修改 Scanner Buffer 常量

**Files:**
- Modify: `internal/engine/types.go:21-25`

- [ ] **Step 1: 查看现有常量**

```bash
cat -n internal/engine/types.go | head -30
```

- [ ] **Step 2: 修改常量**

```go
// 修改前:
const (
    ScannerInitialBufSize = 256 * 1024       // 256 KB
    ScannerMaxBufSize     = 10 * 1024 * 1024 // 10 MB
)

// 修改后:
const (
    ScannerInitialBufSize = 4 * 1024       // 4 KB
    ScannerMaxBufSize     = 512 * 1024    // 512 KB
)
```

- [ ] **Step 3: 运行测试验证**

```bash
go test ./internal/engine/... -v -run "TestSession" 2>&1 | head -50
```

- [ ] **Step 4: 提交**

```bash
git add internal/engine/types.go
git commit -m "perf(engine): reduce scanner buffer from 256KB to 4KB"
```

---

## Chunk 2: 字符串拼接优化

### Task 2: 优化 pool.go 字符串拼接

**Files:**
- Modify: `internal/engine/pool.go:236`

- [ ] **Step 1: 查看当前代码**

```bash
sed -n '230,250p' internal/engine/pool.go
```

- [ ] **Step 2: 修改字符串拼接**

```go
// 修改前:
uniqueStr := fmt.Sprintf("%s:session:%s", sm.opts.Namespace, sessionID)

// 修改后:
uniqueStr := sm.opts.Namespace + ":session:" + sessionID
```

- [ ] **Step 3: 验证编译**

```bash
go build ./internal/engine/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/engine/pool.go
git commit -m "perf(engine): use direct string concat instead of fmt.Sprintf"
```

---

## Chunk 3: Cleanup Loop 自适应触发

### Task 3: 添加 cleanupInterval 方法并修改 cleanupLoop

**Files:**
- Modify: `internal/engine/types.go:27-31` (删除 CleanupCheckInterval)
- Modify: `internal/engine/pool.go:452-465` (修改 cleanupLoop)

- [ ] **Step 1: 删除 types.go 固定常量**

```go
// 删除这一行:
// CleanupCheckInterval = 1 * time.Minute
```

- [ ] **Step 2: 在 pool.go 添加 cleanupInterval 方法**

在 `cleanupLoop` 函数前添加:

```go
// cleanupInterval returns the dynamic interval for cleanup checks.
// It scales with the session timeout: interval = timeout / 4,
// clamped to [1min, 5min].
func (sm *SessionPool) cleanupInterval() time.Duration {
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

- [ ] **Step 3: 修改 cleanupLoop 使用动态间隔**

```go
// 修改前:
func (sm *SessionPool) cleanupLoop() {
    ticker := time.NewTicker(CleanupCheckInterval)
    defer ticker.Stop()

// 修改后:
func (sm *SessionPool) cleanupLoop() {
    ticker := time.NewTicker(sm.cleanupInterval())
    defer ticker.Stop()
```

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/engine/... -v -race 2>&1 | tail -30
```

- [ ] **Step 5: 提交**

```bash
git add internal/engine/types.go internal/engine/pool.go
git commit -m "perf(engine): add adaptive cleanup interval based on timeout"
```

---

## Chunk 4: 验证与收尾

- [ ] **Step 1: 运行完整测试套件**

```bash
go test ./... -count=1 2>&1 | tail -20
```

- [ ] **Step 2: 运行 lint**

```bash
go vet ./internal/engine/...
```

- [ ] **Step 3: 查看 diff**

```bash
git diff main -- internal/engine/
```

- [ ] **Step 4: 推送分支**

```bash
git push origin feat/engine-memory-cpu-optimization
```

- [ ] **Step 5: 创建 PR**

```bash
gh pr create --title "perf(engine): memory and CPU optimization for core engine" --body "$(cat <<'EOF'
## Summary
- Reduce scanner buffer from 256KB to 4KB (98% reduction for small outputs)
- Replace fmt.Sprintf with direct string concatenation
- Add adaptive cleanup interval based on session timeout

## Test plan
- [x] go test ./internal/engine/... passes
- [x] go vet passes

Resolves: (create issue first if needed)
EOF
)"
```
