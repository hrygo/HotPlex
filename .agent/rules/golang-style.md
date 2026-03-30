---
paths:
  - "**/*.go"
---

# Uber Go Style Guide

## 格式化
- 行宽软限制 99 字符
- **两个 import group**：标准库 → 第三方库，无空行分隔
- 相似声明分组：`const`、`var`、类型各自集中
- 避免不必要的 import alias
- 减少嵌套：**优先 early return**
- `gofmt -s` 格式化

## 变量与类型
- 短声明用 `:=`，零值初始化用 `var`
- struct 初始化**使用 field name**
- **省略零值字段**，除非有特殊含义
- 空 map 用 `make(map[K]V)`，有初始数据用字面量
- slice 容量已知时 `make([]T, 0, cap)` 预分配
- 枚举从 **1 开始**（避免零值歧义）
- **瞬间用 `time.Time`**，**时长用 `time.Duration`**
- 原子操作用 `go.uber.org/atomic`

## 接口
- **接口值传递**，不要传指针
- 编译时验证实现：`var _ Worker = (*ClaudeCodeWorker)(nil)`
- receiver：value receiver 可接收值/指针，pointer receiver 只能接收指针

## 错误处理
- 静态错误：`errors.New("session not found")`
- 动态错误：`fmt.Errorf("session %s: %w", id, err)`（保留错误链）
- 错误变量前缀 `Err`，自定义类型后缀 `Error`（如 `SessionNotFoundError`）
- 每个错误**只处理一次**：不同时 log 又 return
- 避免堆叠 "failed to"
- `printf` 格式化字符串用 `const`

## 进程与并发
- **边界处复制 slice/map**，防止外部意外修改
- `sync.Mutex` / `sync.RWMutex` 零值即可，**禁止指针传递**，**禁止 embedding**，**显式命名** `mu`
- `exec.CommandContext` 传递 ctx，goroutine 监听 `ctx.Done()`
- nil slice 检查用 `len(s) == 0`

## 其他
- 尽可能**避免 `init()`**，保持行为确定性
- 使用 `testify/require` 而非 `t.Fatal`
- Functional Options 用于配置类 API：
  ```go
  type Option func(*Config)
  func WithTimeout(d time.Duration) Option
  ```
