# HotPlex 清洁架构重构实施计划

> **版本**: 1.0 | **创建日期**: 2026-02-21
> **方案类型**: Clean Architecture + Provider 集成
> **执行方式**: 单个 PR

---

## 1. 目标结构

```
HotPlex/
├── cmd/
│   └── hotplexd/
│       └── main.go                     # [保持] 入口点
│
├── internal/
│   ├── engine/                         # [新建] 会话池与状态机
│   │   ├── pool.go                     # SessionPool 实现
│   │   ├── pool_test.go
│   │   ├── session.go                  # Session 结构
│   │   ├── session_test.go
│   │   ├── types.go                    # 内部类型 SessionParams
│   │   └── doc.go
│   │
│   ├── security/                       # [新建] WAF 与危险检测
│   │   ├── detector.go                 # Detector 实现
│   │   ├── detector_test.go
│   │   ├── patterns.go                 # 危险模式定义（可选拆分）
│   │   └── doc.go
│   │
│   ├── sys/                            # [新建] 跨平台进程管理
│   │   ├── proc_unix.go                # Unix PGID 信号处理
│   │   ├── proc_windows.go             # Windows 进程管理
│   │   ├── proc_test.go
│   │   └── doc.go
│   │
│   ├── server/                         # [保持] WebSocket 服务
│   │   ├── websocket.go
│   │   ├── websocket_test.go
│   │   └── cors.go
│   │
│   └── strutil/                        # [保持] 字符串工具
│       ├── truncate.go
│       └── truncate_test.go
│
├── provider/                           # [新建] AI CLI 适配层
│   ├── provider.go                     # Provider 接口定义
│   ├── provider_test.go
│   ├── meta.go                         # ProviderMeta, ProviderFeatures
│   ├── config.go                       # ProviderConfig, OpenCodeConfig
│   ├── factory.go                      # ProviderFactory, ProviderRegistry
│   ├── factory_test.go
│   ├── event.go                        # ProviderEvent, 事件类型
│   ├── event_test.go
│   ├── converter.go                    # 事件转换器（关键！）
│   ├── claude/                         # Claude Code 适配器
│   │   ├── claude.go
│   │   ├── claude_test.go
│   │   └── doc.go
│   └── opencode/                       # OpenCode 适配器
│       ├── opencode.go
│       ├── opencode_test.go
│       └── doc.go
│
├── types.go                            # [保持] 公开类型 Config, Usage
├── types_test.go
├── events.go                           # [保持] StreamMessage, EventWithMeta
├── events_test.go
├── errors.go                           # [保持] 错误定义
├── stats.go                            # [保持] 统计类型
├── stats_test.go
├── client.go                           # [保持] HotPlexClient 接口
├── runner.go                           # [重构] Engine 入口 + 转换层
├── runner_test.go
├── doc.go                              # [保持] 包文档
├── testutils_test.go                   # [保持] 测试工具
│
├── go.mod
├── Makefile
└── README.md
```

---

## 2. 依赖关系设计

### 2.1 依赖方向（严格遵守）

```
                    ┌──────────────┐
                    │   用户代码    │
                    └──────┬───────┘
                           │ import
                           ▼
┌──────────────────────────────────────────────────────────┐
│                    hotplex (root)                         │
│  types.go, events.go, errors.go, client.go, runner.go    │
│                    ↓ imports                              │
│         provider/ + internal/*                            │
└──────────────────────────────────────────────────────────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
    ┌────────────┐  ┌────────────┐  ┌────────────┐
    │  provider  │  │   engine   │  │  security  │
    │ (协议适配)  │  │  (会话池)  │  │   (WAF)    │
    └─────┬──────┘  └─────┬──────┘  └────────────┘
          │               │
          └───────┬───────┘
                  │ internal/ 不可逆向依赖根包
                  ▼
           ┌────────────┐
           │    sys     │
           │  (进程)    │
           └────────────┘
```

### 2.2 转换层模式

```go
// runner.go (编排层)
func (e *Engine) Execute(ctx context.Context, cfg *Config, prompt string, callback Callback) error {
    // 1. 转换：公开类型 → 私有参数
    params := engine.SessionParams{
        SessionID: cfg.SessionID,
        WorkDir:   cfg.WorkDir,
        Provider:  e.provider,
    }

    // 2. 调用内部实现
    session, err := e.pool.GetOrCreate(ctx, params)

    // 3. 转换：私有事件 → 公开事件
    return e.bridgeEvents(session, callback)
}
```

---

## 3. 分阶段实施计划

### Phase 1: 创建目录结构

```bash
mkdir -p internal/engine internal/security internal/sys
mkdir -p provider/claude provider/opencode
```

### Phase 2: 底层迁移 (sys + security)

#### 2.1 迁移 sys_*.go

| 操作 | 源文件 | 目标位置 | 包名变更 |
|:-----|:-------|:---------|:---------|
| Move | `sys_unix.go` | `internal/sys/proc_unix.go` | `hotplex` → `sys` |
| Move | `sys_windows.go` | `internal/sys/proc_windows.go` | `hotplex` → `sys` |
| Move | `sys_test.go` | `internal/sys/proc_test.go` | `hotplex` → `sys` |
| Create | - | `internal/sys/doc.go` | - |

#### 2.2 迁移 danger.go

| 操作 | 源文件 | 目标位置 | 包名变更 |
|:-----|:-------|:---------|:---------|
| Move | `danger.go` | `internal/security/detector.go` | `hotplex` → `security` |
| Move | `danger_test.go` | `internal/security/detector_test.go` | `hotplex` → `security` |
| Create | - | `internal/security/doc.go` | - |

#### 2.3 更新导入

```go
// runner.go 和 session_manager.go 更新导入
import (
    "github.com/hrygo/hotplex/internal/security"
    "github.com/hrygo/hotplex/internal/sys"
)
```

### Phase 3: Provider 层组织

#### 3.1 迁移 Provider 文件

| 操作 | 源文件 | 目标位置 | 包名变更 |
|:-----|:-------|:---------|:---------|
| Move | `provider.go` | `provider/provider.go` | `hotplex` → `provider` |
| Move | `provider_event.go` | `provider/event.go` | `hotplex` → `provider` |
| Move | `provider_factory.go` | `provider/factory.go` | `hotplex` → `provider` |
| Move | `provider_claude.go` | `provider/claude/claude.go` | `hotplex` → `claude` |
| Move | `provider_opencode.go` | `provider/opencode/opencode.go` | `hotplex` → `opencode` |
| Move | `provider_test.go` | `provider/provider_test.go` | `hotplex` → `provider` |
| Create | - | `provider/meta.go` | - |
| Create | - | `provider/config.go` | - |
| Create | - | `provider/converter.go` | - |
| Create | - | `provider/doc.go` | - |
| Create | - | `provider/claude/doc.go` | - |
| Create | - | `provider/opencode/doc.go` | - |

#### 3.2 拆分 provider.go

```go
// provider/provider.go - 保留接口和核心类型
package provider

type Provider interface { ... }
type ProviderType string
type ProviderSessionOptions struct { ... }

// provider/meta.go - 提取元数据类型
package provider

type ProviderMeta struct { ... }
type ProviderFeatures struct { ... }

// provider/config.go - 提取配置类型
package provider

type ProviderConfig struct { ... }
type OpenCodeConfig struct { ... }
```

#### 3.3 更新 claude/opencode 包

```go
// provider/claude/claude.go
package claude

import "github.com/hrygo/hotplex/provider"

type ClaudeCodeProvider struct {
    provider.ProviderBase  // 嵌入基础实现
    // ...
}
```

### Phase 4: 会话引擎迁移

#### 4.1 迁移 session_manager.go

| 操作 | 源文件 | 目标位置 | 包名变更 |
|:-----|:-------|:---------|:---------|
| Move | `session_manager.go` | `internal/engine/pool.go` | `hotplex` → `engine` |
| Split | Session 结构 | `internal/engine/session.go` | `engine` |
| Move | `session_manager_test.go` | `internal/engine/pool_test.go` | `engine` |
| Move | `session_test.go` | `internal/engine/session_test.go` | `engine` |
| Move | `session_manager_concurrency_test.go` | `internal/engine/concurrency_test.go` | `engine` |
| Create | - | `internal/engine/types.go` | - |
| Create | - | `internal/engine/doc.go` | - |

#### 4.2 定义内部类型

```go
// internal/engine/types.go
package engine

import (
    "time"
    "github.com/hrygo/hotplex/provider"
)

// SessionParams 内部会话参数（避免依赖根包 Config）
type SessionParams struct {
    SessionID      string
    WorkDir        string
    Provider       provider.Provider
    Namespace      string
    IdleTimeout    time.Duration
    SystemPrompt   string
    PermissionMode string
    AllowedTools   []string
    DisallowedTools []string
}

// SessionStatus 会话状态
type SessionStatus string

const (
    StatusStarting SessionStatus = "starting"
    StatusReady    SessionStatus = "ready"
    StatusBusy     SessionStatus = "busy"
    StatusDead     SessionStatus = "dead"
)

// Callback 内部回调类型
type Callback func(eventType string, data any) error
```

### Phase 5: 重构 runner.go

#### 5.1 添加转换层

```go
// runner.go 中的转换函数

// toSessionParams 转换公开 Config 到内部 SessionParams
func (e *Engine) toSessionParams(cfg *Config) engine.SessionParams {
    return engine.SessionParams{
        SessionID:       cfg.SessionID,
        WorkDir:         cfg.WorkDir,
        Provider:        e.provider,
        Namespace:       e.opts.Namespace,
        IdleTimeout:     e.opts.IdleTimeout,
        SystemPrompt:    e.opts.BaseSystemPrompt,
        PermissionMode:  e.opts.PermissionMode,
        AllowedTools:    e.opts.AllowedTools,
        DisallowedTools: e.opts.DisallowedTools,
    }
}

// toPublicEvent 转换内部事件到公开事件
func (e *Engine) toPublicEvent(pe *provider.ProviderEvent) *EventWithMeta {
    return &EventWithMeta{
        EventType: string(pe.Type),
        EventData: pe.Content,
        Meta: &EventMeta{
            ToolName:        pe.ToolName,
            ToolID:          pe.ToolID,
            Status:          pe.Status,
            DurationMs:      pe.Metadata.DurationMs,
            InputTokens:     pe.Metadata.InputTokens,
            OutputTokens:    pe.Metadata.OutputTokens,
        },
    }
}
```

#### 5.2 更新 Engine 结构

```go
// runner.go
package hotplex

import (
    "github.com/hrygo/hotplex/internal/engine"
    "github.com/hrygo/hotplex/internal/security"
    "github.com/hrygo/hotplex/provider"
)

type Engine struct {
    opts           EngineOptions
    logger         *slog.Logger

    // 重构后的依赖
    pool           *engine.Pool           // 替代 manager SessionManager
    provider       provider.Provider      // 新增：当前 Provider
    providerReg    *provider.Registry     // 新增：Provider 注册表
    dangerDetector *security.Detector     // 从 *Detector 改为 internal

    statsMu        sync.RWMutex
    currentStats   *SessionStats
}
```

### Phase 6: 测试与验证

#### 6.1 更新测试导入

所有测试文件需要更新导入路径：
- `danger_test.go` → `internal/security/detector_test.go`
- `session_*_test.go` → `internal/engine/*_test.go`
- `provider_test.go` → `provider/provider_test.go`

#### 6.2 验证命令

```bash
# 1. 构建验证
go build ./...

# 2. 测试验证
go test -race ./...

# 3. 循环依赖检查
go list -f '{{.ImportPath}}: {{.Imports}}' ./... | grep -E 'internal.*hotplex"'

# 4. 覆盖率
go test -cover ./...

# 5. Lint
golangci-lint run
```

---

## 4. 文件变更清单

### 4.1 新建文件 (16 个)

| 文件 | 说明 |
|:-----|:-----|
| `internal/engine/doc.go` | 包文档 |
| `internal/engine/types.go` | 内部类型定义 |
| `internal/security/doc.go` | 包文档 |
| `internal/security/patterns.go` | 危险模式（可选） |
| `internal/sys/doc.go` | 包文档 |
| `provider/doc.go` | 包文档 |
| `provider/meta.go` | ProviderMeta 类型 |
| `provider/config.go` | ProviderConfig 类型 |
| `provider/converter.go` | 事件转换器 |
| `provider/claude/doc.go` | 包文档 |
| `provider/claude/claude_test.go` | 测试文件 |
| `provider/opencode/doc.go` | 包文档 |
| `provider/opencode/opencode_test.go` | 测试文件 |
| `provider/event_test.go` | 测试文件 |
| `provider/factory_test.go` | 测试文件 |

### 4.2 移动文件 (12 个)

| 源文件 | 目标位置 |
|:-------|:---------|
| `sys_unix.go` | `internal/sys/proc_unix.go` |
| `sys_windows.go` | `internal/sys/proc_windows.go` |
| `sys_test.go` | `internal/sys/proc_test.go` |
| `danger.go` | `internal/security/detector.go` |
| `danger_test.go` | `internal/security/detector_test.go` |
| `session_manager.go` | `internal/engine/pool.go` |
| `session_manager_test.go` | `internal/engine/pool_test.go` |
| `session_test.go` | `internal/engine/session_test.go` |
| `session_manager_concurrency_test.go` | `internal/engine/concurrency_test.go` |
| `provider.go` | `provider/provider.go` |
| `provider_event.go` | `provider/event.go` |
| `provider_factory.go` | `provider/factory.go` |

### 4.3 重构文件 (2 个)

| 文件 | 变更说明 |
|:-----|:---------|
| `runner.go` | 添加转换层、更新导入、集成 Provider |
| `runner_test.go` | 更新导入和类型引用 |

### 4.4 保持不变 (10 个)

| 文件 | 说明 |
|:-----|:-----|
| `types.go` | 公开类型定义 |
| `events.go` | 公开事件类型 |
| `errors.go` | 公开错误定义 |
| `stats.go` | 统计类型 |
| `client.go` | HotPlexClient 接口 |
| `doc.go` | 包文档 |
| `testutils_test.go` | 测试工具 |
| `types_test.go` | 类型测试 |
| `events_test.go` | 事件测试 |
| `stats_test.go` | 统计测试 |

---

## 5. 验收标准

### 5.1 结构验收

- [ ] 根目录 Go 文件 ≤ 10 个
- [ ] `internal/` 包含 4 个子包 (engine, security, sys, server)
- [ ] `provider/` 包含 2 个子包 (claude, opencode)
- [ ] 测试文件跟随源码
- [ ] 无循环依赖

### 5.2 质量验收

- [ ] `go test -race ./...` 通过
- [ ] 覆盖率 ≥ 70%
- [ ] `go vet ./...` 无警告
- [ ] `golangci-lint run` 通过

### 5.3 功能验收

- [ ] Provider 集成到 Engine
- [ ] 公开 API 不变（用户代码无需修改）
- [ ] 示例程序正常运行

---

## 6. 回滚方案

如果重构失败，可通过以下步骤回滚：

```bash
# 1. 回滚到重构前的 commit
git revert HEAD

# 或 2. 硬重置到重构前的分支
git reset --hard origin/main
```

---

> **创建时间**: 2026-02-21
> **作者**: Claude Code
> **状态**: Ready for Implementation
