---
paths:
  - "**/worker/**/*.go"
  - "**/session/**/*.go"
---

# 进程管理规范

## 启动
```go
cmd := exec.CommandContext(ctx, binary, args...)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // PGID 隔离
cmd.Env = append(os.Environ(), extraEnv...)
// 移除 CLAUDECODE= 防止嵌套
```

### P3: macOS 平台兼容性

**内存限制设置需要平台检测**：
```go
// P3 Fix: RLIMIT_AS 只在支持的平台上设置
if runtime.GOOS != "darwin" && cmd.Process != nil {
    const memLimit = 512 * 1024 * 1024 // 512 MB
    if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
        Cur: memLimit,
        Max: memLimit,
    }); err != nil {
        m.log.Warn("proc: setrlimit RLIMIT_AS failed", "error", err)
        // Non-fatal: log and continue
    }
}
```

**设计原因**：
- macOS 不支持 `RLIMIT_AS`（地址空间限制）
- 在 macOS 上调用会返回 `invalid argument` 错误
- 只在 Linux/POSIX 系统上设置（ Darwin 除外）
- 警告级别日志，不影响进程启动

**相关文件**：`internal/worker/proc/manager.go:138`

## Stdin / Stdout
- stdin 写入：JSON + `\n`
- stdout：`bufio.Scanner`，初始 64KB，上限 10MB（超限 → `WORKER_OUTPUT_LIMIT` error）

## 分层终止（必须严格遵循）
1. `syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)` — 优雅终止
2. 等待最多 5s
3. `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` — 强制终止

## Goroutine 泄漏防护
每个启动的 goroutine 必须有明确退出路径：
- ctx cancel：`select { case <-ctx.Done(): return; default: }`
- channel close：sender 关闭，receiver 用 `range` 或 `for v := range ch`
- `sync.WaitGroup`：启动时 `wg.Add(1)`，退出时 `wg.Done()`

## exec.Cmd 清理
```go
// 存活判断
cmd.ProcessState == nil  // true = 存活

// 兜底清理（defer）
defer func() {
    if cmd.ProcessState == nil {
        _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
    }
}()
```
