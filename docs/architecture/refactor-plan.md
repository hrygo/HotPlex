# HotPlex 项目结构重构方案 v2.0

> **版本**: 2.0 | **更新日期**: 2026-02-21
> **参考**: [Go Project Layout Standards](https://github.com/golang-standards/project-layout) | [Best Practices 2025](https://go.dev/blog/package-names)

---

## 1. 现状分析

### 1.1 当前问题

| 问题 | 现状 | 影响 |
|:-----|:-----|:-----|
| **根目录扁平** | 31 个 Go 文件（16 源码 + 15 测试） | 难以定位，认知负担高 |
| **职责混杂** | `danger.go`, `provider*.go`, `session*.go` 同级 | 违反单一职责 |
| **测试散落** | 15 个 `*_test.go` 在根目录 | 测试与源码分离 |
| **internal 未充分利用** | 仅 `strutil/`, `server/` | 核心逻辑暴露过多 |

### 1.2 当前文件清单

```
根目录 Go 文件（按职责分类）:

[公开 API - 保留]
  client.go, runner.go, types.go, events.go, errors.go, doc.go, stats.go

[Provider 层 - 需要整理]
  provider.go, provider_claude.go, provider_opencode.go,
  provider_event.go, provider_factory.go

[内部实现 - 应迁移]
  session_manager.go → internal/engine/
  danger.go          → internal/security/
  sys_unix.go        → internal/sys/
  sys_windows.go     → internal/sys/

[测试文件 - 应跟随源码]
  *_test.go (15个) → 跟随对应源文件
```

---

## 2. 目标结构设计

### 2.1 核心原则

```
┌─────────────────────────────────────────────────────────────┐
│                    设计哲学                                   │
├─────────────────────────────────────────────────────────────┤
│ 1. "Public Thin, Private Thick" - 公开层薄，私有层厚          │
│ 2. "Feature over Layer" - 按功能组织，而非技术分层            │
│ 3. "Test Where Code Lives" - 测试跟随源码                    │
│ 4. "No Circular Deps" - internal 永不引用根包                │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 目标目录结构

```
HotPlex/
├── cmd/                          # 可执行程序入口
│   └── hotplexd/
│       └── main.go
│
├── internal/                     # 私有实现（外部不可导入）
│   ├── engine/                   # [核心] 会话池与状态机
│   │   ├── pool.go               # SessionPool 实现
│   │   ├── pool_test.go
│   │   ├── session.go            # Session 结构
│   │   ├── session_test.go
│   │   └── doc.go
│   │
│   ├── security/                 # [安全] WAF 与危险检测
│   │   ├── detector.go           # Detector 实现
│   │   ├── detector_test.go
│   │   ├── patterns.go           # 危险模式定义
│   │   └── doc.go
│   │
│   ├── sys/                      # [底层] 跨平台进程管理
│   │   ├── proc_unix.go          # Unix PGID 信号处理
│   │   ├── proc_windows.go       # Windows 进程管理
│   │   ├── proc_test.go
│   │   └── doc.go
│   │
│   ├── server/                   # [适配器] WebSocket 服务
│   │   ├── websocket.go
│   │   ├── websocket_test.go
│   │   ├── cors.go
│   │   └── doc.go
│   │
│   └── strutil/                  # [工具] 字符串工具
│       ├── truncate.go
│       └── truncate_test.go
│
├── provider/                     # [协议层] AI CLI 适配（公开但独立）
│   ├── provider.go               # Provider 接口定义
│   ├── provider_test.go
│   ├── meta.go                   # ProviderMeta, ProviderFeatures
│   ├── config.go                 # ProviderConfig, OpenCodeConfig
│   ├── factory.go                # ProviderFactory, ProviderRegistry
│   ├── factory_test.go
│   ├── event.go                  # ProviderEvent, 事件类型
│   ├── event_test.go
│   ├── claude/                   # Claude Code 适配器
│   │   ├── claude.go
│   │   ├── claude_test.go
│   │   └── doc.go
│   └── opencode/                 # OpenCode 适配器
│       ├── opencode.go
│       ├── opencode_test.go
│       └── doc.go
│
├── types.go                      # [公开] 核心类型 Config, Usage
├── types_test.go
├── events.go                     # [公开] StreamMessage, EventWithMeta
├── events_test.go
├── errors.go                     # [公开] 错误定义
├── stats.go                      # [公开] 统计类型
├── client.go                     # [公开] HotPlexClient 接口
├── runner.go                     # [公开] Engine 入口（编排层）
├── runner_test.go
├── doc.go                        # [公开] 包文档
├── testutils_test.go             # [测试] 共享测试工具
│
├── api/                          # [未来] OpenAPI/Proto 定义
├── configs/                      # [配置] 示例配置文件
├── _examples/                    # [示例] 使用示例
├── docs/                         # [文档] 架构文档
├── scripts/                      # [脚本] 构建/部署脚本
│
├── go.mod
├── Makefile
└── README.md
```

### 2.3 包职责映射

| 包路径 | 职责 | 可见性 | 依赖方向 |
|:-------|:-----|:-------|:---------|
| `hotplex` (root) | 公开 API，类型定义 | Public | ← 用户导入 |
| `hotplex/provider` | AI CLI 协议适配 | Public | → internal/sys |
| `internal/engine` | 会话池，状态机 | Private | → internal/security, internal/sys |
| `internal/security` | 危险检测 WAF | Private | 无外部依赖 |
| `internal/sys` | 进程组，信号处理 | Private | 无外部依赖 |
| `internal/server` | WebSocket 适配器 | Private | → hotplex (接口) |
| `internal/strutil` | 字符串工具 | Private | 无外部依赖 |

---

## 3. 依赖关系设计

### 3.1 依赖图

```
                    ┌──────────────┐
                    │   用户代码    │
                    └──────┬───────┘
                           │ import
                           ▼
┌──────────────────────────────────────────────────────────┐
│                    hotplex (root)                         │
│  types.go, events.go, errors.go, client.go, runner.go    │
└──────────────────────────┬───────────────────────────────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
    ┌────────────┐  ┌────────────┐  ┌────────────┐
    │  provider  │  │   runner   │  │   server   │
    │ (协议适配)  │  │  (编排层)   │  │ (WebSocket)│
    └─────┬──────┘  └─────┬──────┘  └─────┬──────┘
          │               │               │
          └───────────────┼───────────────┘
                          │ internal/ 不可逆向依赖
                          ▼
┌──────────────────────────────────────────────────────────┐
│                      internal/                            │
│  ┌─────────┐  ┌──────────┐  ┌─────────┐  ┌─────────┐    │
│  │ engine  │  │ security │  │   sys   │  │ strutil │    │
│  │(会话池) │  │  (WAF)   │  │ (进程)  │  │ (工具)  │    │
│  └────┬────┘  └────┬─────┘  └────┬────┘  └─────────┘    │
│       └────────────┴─────────────┘                       │
└──────────────────────────────────────────────────────────┘
```

### 3.2 转换层模式 (Conversion Layer)

消除 `internal` 对 `hotplex` 的依赖：

```go
// runner.go (编排层)
func (e *Engine) Execute(ctx context.Context, cfg Config) (*Result, error) {
    // 转换：公开类型 → 私有参数
    params := engine.SessionParams{
        WorkDir:       cfg.WorkDir,
        Prompt:        cfg.Prompt,
        DangerEnabled: cfg.BypassDanger,
    }

    // 调用内部实现
    session, err := e.pool.GetOrCreate(params)
    if err != nil {
        return nil, err
    }

    // 转换：私有事件 → 公开事件
    return e.bridgeEvents(session), nil
}
```

---

## 4. 实施路线图

### Phase 0: 准备工作 (Day 0)

```bash
# 1. 创建目标目录结构
mkdir -p internal/{engine,security,sys}
mkdir -p provider/{claude,opencode}

# 2. 确保测试通过
go test -race ./...
```

### Phase 1: 底层迁移 (Day 1-2)

**目标**: 迁移无外部依赖的原子模块

| 操作 | 源文件 | 目标位置 |
|:-----|:-------|:---------|
| Move | `sys_unix.go`, `sys_windows.go` | `internal/sys/proc_*.go` |
| Move | `danger.go` | `internal/security/detector.go` |
| Move | `danger_test.go` | `internal/security/detector_test.go` |
| Refactor | 提取危险模式 | `internal/security/patterns.go` |

**验证**:
```bash
go test -race ./internal/sys/...
go test -race ./internal/security/...
```

### Phase 2: 会话引擎迁移 (Day 3-4)

**目标**: 迁移核心会话管理逻辑

| 操作 | 源文件 | 目标位置 |
|:-----|:-------|:---------|
| Move | `session_manager.go` | `internal/engine/pool.go` |
| Split | Session 结构 | `internal/engine/session.go` |
| Move | `session_*_test.go` | `internal/engine/*_test.go` |
| Create | 转换层 | `runner.go` 更新 |

**关键变更**:
- `SessionPool` → `engine.Pool`
- `Session` → `engine.Session`
- 定义 `engine.SessionParams` 替代 `hotplex.Config` 传入

### Phase 3: Provider 层重构 (Day 5-6)

**目标**: 组织 Provider 层，提升可扩展性

| 操作 | 源文件 | 目标位置 |
|:-----|:-------|:---------|
| Move | `provider.go` | `provider/provider.go` |
| Move | `provider_event.go` | `provider/event.go` |
| Move | `provider_factory.go` | `provider/factory.go` |
| Move | `provider_claude.go` | `provider/claude/claude.go` |
| Move | `provider_opencode.go` | `provider/opencode/opencode.go` |
| Extract | ProviderMeta, Config | `provider/meta.go`, `provider/config.go` |

**注意**: `provider/` 保持公开，允许外部扩展新 Provider。

### Phase 4: 根目录清理 (Day 7)

**目标**: 精简根目录，仅保留公开 API

| 操作 | 说明 |
|:-----|:-----|
| Keep | `types.go`, `events.go`, `errors.go`, `stats.go`, `client.go`, `runner.go`, `doc.go` |
| Move Tests | 测试文件跟随源码移动 |
| Clean | 删除根目录下所有已迁移文件 |
| Update | `doc.go` 更新包文档 |

**最终根目录文件清单**:
```
根目录 Go 文件（目标：8-10个）
├── client.go          # 公开客户端接口
├── runner.go          # Engine 入口
├── types.go           # 核心类型
├── events.go          # 事件定义
├── errors.go          # 错误定义
├── stats.go           # 统计类型
├── doc.go             # 包文档
└── testutils_test.go  # 测试工具
```

### Phase 5: 验证与文档 (Day 8)

- [ ] 全量测试: `go test -race -cover ./...`
- [ ] 构建验证: `go build ./...`
- [ ] 更新 `AGENT.md` 架构说明
- [ ] 更新 `README.md` 导入示例
- [ ] 创建 `MIGRATION.md` 升级指南

---

## 5. API 兼容性保障

### 5.1 用户导入路径（不变）

```go
// 用户代码 - 导入路径保持不变
import "github.com/hrygo/hotplex"

func main() {
    engine, _ := hotplex.NewEngine(hotplex.EngineOptions{...})
    result, _ := engine.Execute(ctx, hotplex.Config{...})
}
```

### 5.2 内部包（不可导入）

```go
// 外部无法导入 - Go 编译器强制
import "github.com/hrygo/hotplex/internal/engine" // ❌ 编译错误
```

### 5.3 Provider 扩展（公开）

```go
// 外部可实现自定义 Provider
import "github.com/hrygo/hotplex/provider"

type MyCustomProvider struct {
    provider.ProviderBase
}

func (p *MyCustomProvider) BuildCLIArgs(...) []string {
    // 自定义实现
}
```

---

## 6. 风险评估与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|:-----|:-----|:-----|:---------|
| 循环依赖 | 中 | 高 | 严格遵循转换层模式 |
| 测试失败 | 低 | 高 | 每阶段验证，增量迁移 |
| API 破坏 | 低 | 高 | 根目录公开接口签名不变 |
| 性能回归 | 低 | 中 | 转换层仅做类型映射，无额外开销 |

---

## 7. 验收标准

### 7.1 结构验收

- [ ] 根目录 Go 文件 ≤ 10 个
- [ ] `internal/` 包含 4+ 子包
- [ ] 测试文件跟随源码
- [ ] 无循环依赖 (`go list` 验证)

### 7.2 质量验收

- [ ] `go test -race ./...` 通过
- [ ] 覆盖率 ≥ 70%
- [ ] `go vet ./...` 无警告
- [ ] `golangci-lint run` 通过

### 7.3 文档验收

- [ ] `doc.go` 更新
- [ ] `README.md` 更新导入示例
- [ ] `MIGRATION.md` 创建

---

## 8. 后续演进方向

### 8.1 短期 (v0.3.x)

- 添加 `api/` 目录存放 OpenAPI 规范
- 支持 gRPC 流式接口
- 添加 `scripts/` 构建脚本

### 8.2 中期 (v0.4.x)

- Provider 插件化支持（动态加载）
- 配置系统重构（Koanf 集成）
- 可观测性增强（Metrics, Tracing）

### 8.3 长期 (v1.0)

- 多节点集群支持
- 会话持久化与恢复
- 企业级安全审计

---

## 参考资源

- [Go Project Layout Standards](https://github.com/golang-standards/project-layout)
- [Go Package Naming Guide](https://go.dev/blog/package-names)
- [Internal Package Pattern](https://go.dev/doc/go1.4#internalpackages)
- [Hexagonal Architecture in Go](https://aliatar.github.io/posts/hexagonal-architecture-in-go/)

---

> **Last Updated**: 2026-02-21
> **Author**: Claude Code + HotPlex Team
> **Status**: Draft for Review
