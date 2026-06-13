# HotPlex 发展路线图（2026 Q3 — 2027 Q1）

> 基于 Codex CLI（Rust, 120+ crates）架构分析，结合 HotPlex 现状制定。
> 核心定位差异：**Codex = 单用户 CLI 工具**；**HotPlex = 多平台多 Agent 编排网关**。

---

## 一、现状对比矩阵

| 能力维度 | Codex CLI | HotPlex 现状 | 差距评级 |
|---------|-----------|-------------|---------|
| **安全沙箱** | 5 层（进程加固→seccomp→Landlock→bubblewrap→Guardian） | 命令/工具/模型白名单、SSRF 防护、路径穿越防护 | 🔴 重大差距 |
| **扩展系统** | Contributor 模式（8 种生命周期 trait，默认空实现） | Skills 文件扫描 + B/C 通道注入 | 🟡 中等差距 |
| **工具可见性** | ToolExposure 枚举（Direct/Deferred/Hidden/DirectModelOnly） | AllowedTools / DisallowedTools 二值白名单 | 🟡 中等差距 |
| **自动审批** | Guardian 断路器（fail-closed，可配置阈值） | 无（权限请求转交用户，无自动审批） | 🔴 重大差距 |
| **异步通信** | SQ/EQ 模式（Submission Queue / Event Queue） | WebSocket AEP + Hub 广播（单方向） | 🟢 已有基础 |
| **Hook 系统** | 10 种事件类型钩子（PreToolUse/PostToolUse/SessionStart…） | 无显式 Hook 架构 | 🟡 中等差距 |
| **分层配置** | TOML + 插件 requirements/constraints | YAML + Viper + 热重载 + 配置继承 | 🟢 已有优势 |
| **传输回退** | WebSocket → SSE + 预热连接 | WebSocket 唯一传输 | 🟠 可改进 |
| **线程/会话模型** | Thread 模型（compaction/resume/fork/archive） | 5 状态机 + 确定性 UUIDv5 + resume | 🟢 已有基础 |
| **插件市场** | 完整插件市场系统 | 无（Skills 目录扫描） | 🔴 重大差距 |
| **Daemon 模式** | App Server + IPC | systemd/launchd/SCM 服务化 | 🟢 已有替代 |
| **分布式追踪** | W3C Trace Context 完整传播 | OpenTelemetry + W3C TraceContext（部分实现） | 🟢 已有基础 |
| **自动压缩** | Token 超预算自动 compact | /compact 用户命令 + history_compress | 🟠 可改进 |
| **代码模式** | 嵌套工具执行（Code Mode） | 无 | 🟡 中等差距 |
| **工具搜索** | 延迟发现（Deferred Discovery） | /skills 静态列表 | 🟠 可改进 |
| **多 Agent UI** | TUI 多 Agent 面板 | WebChat 单会话 | 🟡 中等差距 |
| **语音输入** | Voice Input 支持 | STT/TTS 集成（飞书） | 🟢 已有基础 |
| **SDK** | Python SDK | Go Client + Python/TS/Java 客户端示例 | 🟢 已有基础 |
| **记忆系统** | Memories（read/write） | 无显式 Memory 模块 | 🟡 中等差距 |
| **多平台** | 无（单用户 CLI） | Slack/飞书/原信/WebChat 多平台 | ✅ **独有优势** |
| **Cron 定时** | 无 | 3 种调度 + 3 种负载 + 交付重试 | ✅ **独有优势** |
| **Brain 编排** | 无 | LLM 装饰器链 + 路由 + 熔断 | ✅ **独有优势** |
| **热重载** | 无 | YAML 热重载 + 审计 + 回滚 | ✅ **独有优势** |

---

## 二、HotPlex 独有优势放大策略

HotPlex 不应复制 Codex 的单用户 CLI 路线，而应利用网关定位放大以下差异化优势：

1. **多平台编排枢纽**：Codex 只管一个终端；HotPlex 同时管理 Slack、飞书、WebChat 多渠道
2. **Agent 即服务**：Agent 不绑定用户终端，而是作为后台服务运行，Cron 调度，结果自动回传
3. **多 Agent 协作**：同一会话可由不同 Worker（Claude Code / Codex / OCS / ACP）驱动，统一 AEP 协议
4. **企业级运维**：热重载、30+ Prometheus 指标、OTel 追踪、配置审计——Codex 不需要，企业需要

---

## 三、分阶段路线图

### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
### Phase 1: 安全加固与核心编排（Q3 2026）
### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

> **主题**：缩小最关键差距——安全沙箱与自动化审批，同时建立扩展架构基础。

#### 1.1 Guardian 自动审批引擎

**是什么**：
引入基于规则的自动审批系统，当 Agent 请求执行工具时，根据预设策略自动批准或拒绝，无需人工介入。采用 fail-closed 哲学（不确定时拒绝），防止误操作。

**为什么重要**：
- Cron 任务无人值守运行，无法手动审批权限请求
- 多平台场景下用户不在终端旁，权限请求可能超时
- 自动审批可显著提升 Agent 自主执行效率

**Go 实现方案**：
```
internal/approval/
  guardian.go       # Guardian 结构体：规则引擎 + 断路器
  rule.go           # Rule 接口：Match(session, tool, args) → approve/deny/defer
  rules/            # 内置规则：SafeRead, SafeWrite, NetworkBlock, BudgetCap
  breaker.go        # Breaker：连续拒绝计数 → 熔断（锁定为 deny-all）
  policy.go         # Policy：YAML 策略加载 + 热重载
```

- `Rule` 接口采用 Go 的组合模式，内置规则实现 `Rule` 接口，用户通过 YAML 定义组合策略
- 断路器使用 `atomic.Int32` 追踪连续拒绝，阈值可配
- 策略文件支持热重载（复用 `config.Watcher` 模式）

**涉及模块**：
- `internal/security/tool.go` — 从二值白名单扩展为分级策略
- `pkg/events/events.go` — 新增 `GuardianDecision` 事件类型
- `internal/gateway/bridge.go` — 在 forwardEvents 中拦截 PermissionRequest
- `internal/messaging/interaction.go` — 交互管理器集成自动审批结果

---

#### 1.2 Worker 进程沙箱增强

**是什么**：
在现有命令白名单基础上，增加操作系统级沙箱约束，限制 Worker 进程的文件系统、网络和系统调用访问范围。

**为什么重要**：
- 防止 Agent 意外执行恶意命令（写入敏感路径、访问内网）
- 企业部署需要满足最小权限原则
- Codex 的 5 层沙箱证明了深度防御的必要性

**Go 实现方案**：
```
internal/security/
  sandbox.go        # Sandbox 接口：Apply(cmd *exec.Cmd) error
  sandbox_linux.go  # Linux: Landlock (LIBLANDLOCK) + seccomp-bpf
  sandbox_darwin.go # macOS: sandbox-exec +Seatbelt sandbox profile
  sandbox_stub.go   # Windows/其他: 环境变量隔离 + 路径限制
  sandbox_config.go  # SandboxConfig: 策略定义
```

- 利用 Go 的 `golang.org/x/sys/unix` 调用 Landlock（Linux 5.13+）
- macOS 使用 `sandbox-exec` + 动态生成 seatbelt 配置文件
- 通过 Build Tags 隔离平台实现（`//go:build linux`）

**涉及模块**：
- `internal/worker/proc/manager.go` — 在 `Start()` 中应用沙箱
- `internal/worker/claudecode/worker.go` — 传递 SandboxConfig
- `internal/worker/codexcli/worker.go` — 已有 `Sandbox` 字段，整合
- `internal/config/config.go` — 新增 `Security.Sandbox` 子配置

---

#### 1.3 Hook 生命周期系统

**是什么**：
定义 Agent 执行过程中的可扩展钩子点，允许在 Session 开始/结束、工具调用前后等关键时刻注入自定义逻辑。

**为什么重要**：
- 为扩展系统（Phase 2）提供基础架构
- 允许企业自定义审计、日志、通知等横切关注点
- Codex 的 10 种钩子事件已被证明是实用的扩展模式

**Go 实现方案**：
```go
// internal/hooks/hooks.go
type HookType string
const (
    HookSessionStart   HookType = "session.start"
    HookSessionEnd     HookType = "session.end"
    HookPreToolUse     HookType = "pre_tool_use"
    HookPostToolUse    HookType = "post_tool_use"
    HookPermissionReq  HookType = "permission_request"
    HookError          HookType = "error"
    HookTurnStart      HookType = "turn.start"
    HookTurnEnd        HookType = "turn.end"
    HookContextCompact HookType = "context.compact"
    HookBudgetAlert    HookType = "budget.alert"
)

type Hook interface {
    Type() HookType
    Execute(ctx context.Context, event HookEvent) (HookResult, error)
}

// Register via init() pattern — 与 Worker Registry 同一模式
func Register(h Hook)
```

- 遵循 Codex 的 Contributor 模式：`Hook` 接口 + 默认空实现 `NoopHook`
- 支持优先级排序（`Priority() int`），先注册先执行
- 支持阻断型 Hook（返回 `Abort` 可阻止操作继续）

**涉及模块**：
- 新建 `internal/hooks/` 包
- `internal/gateway/bridge.go` — 在 Session/Turn 生命周期中触发 Hook
- `internal/gateway/forwardEvents` — 在工具调用事件流中触发 Hook
- `pkg/events/events.go` — Hook 事件类型定义

---

#### 1.4 工具可见性分级（ToolExposure）

**是什么**：
将当前的二值工具白名单扩展为四级可见性控制：
- **Direct**：工具直接暴露给用户，可手动调用
- **Deferred**：工具对用户不可见，由 Agent 按需发现
- **Hidden**：工具完全隐藏，仅内部使用
- **ModelOnly**：工具仅模型可见，用户不可见

**为什么重要**：
- MCP 工具数量激增（部分用户配置 50+ MCP Server），需要精细控制
- 减少用户认知负担，只展示相关工具
- 支持 "工具搜索"（延迟发现）能力

**Go 实现方案**：
```go
// internal/security/tool.go 扩展
type ToolExposure string
const (
    ExposureDirect     ToolExposure = "direct"
    ExposureDeferred    ToolExposure = "deferred"
    ExposureHidden      ToolExposure = "hidden"
    ExposureModelOnly   ToolExposure = "model_only"
)

type ToolPolicy struct {
    Name     string
    Exposure ToolExposure
    // 条件：按会话、用户、Bot 动态判定
    Condition func(session *session.SessionInfo) bool
}
```

- `InitConfig.AllowedTools` 保持兼容，新增 `ToolPolicies` 字段
- `handleSkillsList` 根据 Exposure 过滤返回结果

**涉及模块**：
- `internal/security/tool.go` — 核心策略定义
- `internal/gateway/init.go` — InitConfig 扩展
- `pkg/events/events.go` — SkillsListData 增加 exposure 字段
- `internal/gateway/handler.go` — handleSkillsList 逻辑

---

#### 1.5 分布式追踪完善

**是什么**：
完善 W3C TraceContext 在 AEP 协议中的端到端传播，确保从客户端请求到 Worker 工具执行的完整追踪链。

**为什么重要**：
- 已有 OTel 基础设施，但 `TODO` 标注的 `trace_id` 注入尚未实现
- 多平台场景下，追踪链跨越 Slack/飞书 API → Gateway → Worker 进程
- 对企业级排障至关重要

**Go 实现方案**：
```go
// pkg/aep/codec.go 扩展
// 在 Encode 时自动注入 traceparent header
func marshalEnvelope(env *events.Envelope) ([]byte, error) {
    // 从 ctx 提取 trace context
    // 写入 env.Metadata["traceparent"]
}

// internal/gateway/hub.go
// SendToSession 时通过 context.Context 传播 trace
```

- 复用 `go.opentelemetry.io/otel/propagation` 的 W3C propagator
- Worker stdio 通过 JSON-RPC extra 字段传递 traceparent
- ACP worker 的 `trace.go` 已有 trace 采集，需要反向传播

**涉及模块**：
- `pkg/aep/codec.go` — Encode 时注入 traceparent
- `internal/gateway/hub.go` — SendToSession context 传播
- `internal/worker/acp/trace.go` — 现有 trace 采集，扩展传播
- `internal/worker/base/conn.go` — stdin SessionConn 传播

---

### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
### Phase 2: 扩展生态与多 Agent 协作（Q4 2026）
### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

> **主题**：建立可扩展的 Agent 协作框架，让 HotPlex 从"网关"升级为"编排平台"。

#### 2.1 Contributor 扩展架构

**是什么**：
借鉴 Codex 的 Contributor 模式，为 HotPlex 定义一套标准扩展接口，允许第三方开发者以 Go 插件形式扩展网关行为。

**为什么重要**：
- 当前 B/C 通道是静态文件注入，无法动态响应 Agent 执行状态
- 企业需要自定义审批策略、通知方式、审计日志格式
- 开源生态需要标准扩展点才能增长

**Go 实现方案**：
```go
// internal/contributor/contributor.go
type Contributor interface {
    // 生命周期
    Name() string
    Init(ctx context.Context, cfg Config) error
    Shutdown(ctx context.Context) error

    // 可选能力 — 通过类型断言检测
}

// 标准扩展点（对应 Codex 的 8 种 trait）
type SessionLifecycleContributor interface {
    OnSessionStart(ctx context.Context, session *session.SessionInfo) error
    OnSessionEnd(ctx context.Context, session *session.SessionInfo) error
}

type ToolLifecycleContributor interface {
    OnPreToolUse(ctx context.Context, tool ToolCallInfo) (ToolUseDecision, error)
    OnPostToolUse(ctx context.Context, result ToolResultInfo) error
}

type TurnLifecycleContributor interface {
    OnTurnStart(ctx context.Context, turn TurnInfo) error
    OnTurnEnd(ctx context.Context, turn TurnInfo) error
}

type ApprovalReviewContributor interface {
    ReviewApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

type TokenUsageContributor interface {
    OnTokenUsage(ctx context.Context, usage TokenUsageInfo) error
}

type ContextContributor interface {
    ContributeContext(ctx context.Context, session *session.SessionInfo) (string, error)
}

type TurnInputContributor interface {
    TransformInput(ctx context.Context, input string, session *session.SessionInfo) (string, error)
}

type McpServerContributor interface {
    RegisterMCPServers(ctx context.Context) ([]MCPServerConfig, error)
}
```

- 采用与 Worker Registry 相同的 `init()` + blank import 自注册模式
- 每个接口独立，Contributor 只实现需要的接口
- 优先级排序 + 可阻断

**涉及模块**：
- 新建 `internal/contributor/` 包
- `internal/gateway/bridge.go` — Session/Turn 生命周期中调用 Contributor
- `internal/gateway/handler.go` — 工具调用事件中调用 Contributor
- `internal/config/config.go` — `Contributors` 子配置

---

#### 2.2 Memory 持久化系统

**是什么**：
为 Agent 提供跨会话、跨平台的持久化记忆存储。Agent 可以读取和写入结构化记忆，实现"记住用户偏好"、"记住项目约定"等能力。

**为什么重要**：
- 当前每个会话是独立的，Agent 无法"记住"跨会话信息
- Codex 的 Memories 已验证此功能对开发效率的提升
- 多平台场景下记忆需要跨渠道共享（用户在 Slack 提到的偏好，WebChat 也能用）

**Go 实现方案**：
```
internal/memory/
  memory.go         # Memory 接口：Read/Write/Search/List/Delete
  store.go          # Store 接口 + SQLite 实现
  pg_store.go       # PostgreSQL 实现
  namespace.go      # 命名空间隔离（按 user/bot/project）
  injector.go       # B 通道记忆注入（扩展 agentconfig.Writer）
```

```go
type Memory interface {
    Read(ctx context.Context, key string) (string, error)
    Write(ctx context.Context, key, value string) error
    Search(ctx context.Context, query string) ([]MemoryEntry, error)
    List(ctx context.Context, prefix string) ([]MemoryEntry, error)
    Delete(ctx context.Context, key string) error
}

type MemoryEntry struct {
    Key       string    `json:"key"`
    Value     string    `json:"value"`
    Namespace string    `json:"namespace"` // user:xxx, bot:xxx, project:xxx
    UpdatedAt time.Time `json:"updated_at"`
}
```

- 命名空间隔离防止跨用户/跨 Bot 记忆泄漏
- 通过 B 通道注入系统提示词中（扩展 `internal/agentconfig/writer.go`）
- Worker 通过 AEP 的 `tool_call`（读取）和 `worker_command`（写入）操作记忆

**涉及模块**：
- 新建 `internal/memory/` 包
- `internal/agentconfig/writer.go` — B 通道注入记忆内容
- `internal/gateway/bridge_worker.go` — Worker 生命周期初始化记忆
- `internal/session/manager.go` — Session 创建时加载记忆
- `pkg/events/events.go` — 可能新增记忆相关事件类型

---

#### 2.3 多 Agent 协作（Session Federation）

**是什么**：
允许一个用户任务由多个 Agent 协作完成。例如：一个 Agent 负责代码审查，另一个负责测试编写，第三个负责文档生成——HotPlex 作为编排者协调它们的执行。

**为什么重要**：
- 这是 HotPlex 相对于所有 CLI 工具的最大差异化优势
- Codex 的多 Agent 仅限于 TUI 面板展示，实际执行仍是单 Agent
- 企业级场景需要 Agent 分工协作

**Go 实现方案**：
```go
// internal/federation/
// Federation: 多 Worker 协作编排器
type Federation struct {
    sessionID string
    workers   map[string]worker.Worker  // role → worker
    graph     *DAG                      // 任务依赖图
}

type DAG struct {
    nodes []*TaskNode
    edges map[string][]string // nodeID → dependent nodeIDs
}

type TaskNode struct {
    ID       string
    Role     string           // "reviewer", "tester", "writer"
    WorkerType worker.WorkerType
    Prompt   string
    Depends  []string        // 依赖的 node ID
    Status   TaskStatus
    Result   string
}
```

- DAG 定义任务的执行依赖关系
- 支持 fan-out（并行执行）和 fan-in（聚合结果）
- 每个 Task 对应一个底层 Worker Session
- 最终结果聚合后返回给用户

**涉及模块**：
- 新建 `internal/federation/` 包
- `internal/gateway/bridge.go` — 扩展为支持多 Worker 绑定
- `internal/session/manager.go` — 支持 SessionGroup 概念
- `internal/messaging/bridge.go` — 协作结果聚合后推送平台
- `internal/brain/brain.go` — Brain 作为任务分解器

---

#### 2.4 Skill Marketplace 基础

**是什么**：
将当前文件系统的 Skills 扫描升级为可安装、可版本管理的 Skill 包管理器。

**为什么重要**：
- Codex 的插件市场证明了生态化的价值
- HotPlex 的多平台特性使得 Skill 可以"一次编写，多渠道分发"
- 企业可以维护内部 Skill 仓库

**Go 实现方案**：
```
internal/skillmarket/
  registry.go        # Registry: 远程 Skill 索引
  installer.go       # Install/Uninstall/Update
  resolver.go        # 依赖解析（Skill A 依赖 Skill B）
  verifier.go        # 签名验证（可选）
  local_store.go     # 本地已安装 Skill 索引
  config.go          # SkillSource 配置（git repo / OCI / local）
```

- 复用 `internal/skills/scanner.go` 的扫描能力
- Skill 包格式：`.md` + `manifest.yaml` + 可选 `hooks/` 目录
- 支持从 Git 仓库、OCI Registry 或本地目录安装
- `hotplex skill install github.com/org/skill-name`

**涉及模块**：
- `internal/skills/` — 扩展为支持多来源扫描
- `internal/config/config.go` — 新增 `SkillMarket` 配置
- `cmd/hotplex/` — 新增 `skill` 子命令
- `internal/skills/scanner.go` — 扩展 `scanDirs()` 支持安装目录

---

#### 2.5 自动上下文压缩（Auto-Compact）

**是什么**：
当 Worker 的 Token 使用量超过阈值时，Gateway 自动触发上下文压缩，无需用户手动执行 `/compact`。

**为什么重要**：
- 长时间运行的 Cron 任务无法手动触发压缩
- 自动压缩可以避免 Token 超限导致的 API 错误
- Codex 已验证自动压缩的可行性

**Go 实现方案**：
```go
// internal/gateway/bridge.go 扩展
// 在 forwardEvents 中监控 ContextUsage 事件
func (b *Bridge) forwardEvents(ctx context.Context, ...) {
    for env := range recvCh {
        // 检测 context_usage 百分比
        if env.Event.Type == events.ContextUsage {
            usage := parseContextUsage(env)
            if usage.Percentage >= b.cfg.AutoCompactThreshold {
                // 自动触发 compact
                go b.triggerAutoCompact(ctx, sessionID)
            }
        }
    }
}
```

- 阈值可配（默认 80%），每个会话最多自动 compact 一次
- Compact 操作复用现有的 `StdioCompact` WorkerCommand
- 与 Guardian 集成：自动 compact 被视为 Safe 操作

**涉及模块**：
- `internal/gateway/bridge.go` — forwardEvents 中增加 auto-compact 逻辑
- `internal/config/config.go` — 新增 `Gateway.AutoCompactThreshold`
- `internal/gateway/bridge_worker.go` — Compact 命令发送

---

#### 2.6 Token 预算管理

**是什么**：
扩展 `MaxBudgetUSD` 字段为完整的 Token 预算系统，支持按会话、按用户、按 Bot 的多级预算控制和预警。

**为什么重要**：
- 已有 `internal/brain/llm/cost.go` 的 CJK-aware Token 估算基础
- 企业需要控制 API 成本
- Codex 的预算管理相对简单，HotPlex 可做多级更精细

**Go 实现方案**：
```
internal/budget/
  manager.go         # BudgetManager: 多级预算追踪
  tracker.go         # PerSession/PerUser/PerBot 追踪器
  alert.go           # 预算预警（50%/80%/95% 阈值）
  store.go           # 预算历史持久化
```

- 复用 `brain.llm.cost` 的 Token 估算和计费逻辑
- 与 Hook 系统集成：`HookBudgetAlert` 在预算预警时触发
- 预算耗尽时自动终止会话（fail-safe）

**涉及模块**：
- 新建 `internal/budget/` 包
- `internal/brain/llm/cost.go` — 复用 Token 估算
- `internal/gateway/bridge.go` — 会话结束时记录消耗
- `internal/hooks/` — BudgetAlert Hook

---

### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
### Phase 3: 智能编排与生态成熟（Q1 2027）
### ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

> **主题**：将 HotPlex 从 Agent 网关升级为智能编排平台，实现 Agent 自主决策和多 Agent 协同。

#### 3.1 Agent 路由与自动选择

**是什么**：
Brain 模块升级为智能路由器，根据任务特征自动选择最合适的 Worker 类型（Claude Code / Codex / OCS / ACP）执行任务。

**为什么重要**：
- 当前每个 Bot 配置固定绑定一个 Worker 类型
- 不同任务适合不同 Agent（代码编写 vs 代码审查 vs 文档生成）
- 这是 HotPlex 作为"编排平台"的核心能力

**Go 实现方案**：
```go
// internal/brain/router.go 扩展
type AgentRouter interface {
    Route(ctx context.Context, task TaskContext) (RouteDecision, error)
}

type TaskContext struct {
    UserMessage   string
    ProjectType   string   // "go", "python", "typescript"
    FileType      string
    TaskKind      string   // "code", "review", "test", "docs"
    Complexity    string   // "low", "medium", "high"
    Platform      string
    BotName       string
}

type RouteDecision struct {
    WorkerType    worker.WorkerType
    Reason        string
    Confidence    float64
}
```

- 复用 `internal/brain/brain.go` 的 LLM 调用能力
- 轻量规则 + LLM fallback：简单任务规则匹配，复杂任务 LLM 路由
- 路由决策可审计（记录为什么选择了某个 Worker）

**涉及模块**：
- `internal/brain/router.go` — 扩展现有路由器
- `internal/brain/brain.go` — 增加路由专用方法
- `internal/messaging/bridge.go` — 调用路由器选择 Worker
- `internal/gateway/bridge.go` — 动态 Worker 选择

---

#### 3.2 传输回退与连接预热（Transport Fallback）

**是什么**：
为 WebSocket 连接增加 SSE 回退机制，并在空闲时保持预热连接，减少首次连接延迟。

**为什么重要**：
- 部分网络环境（企业代理、防火墙）限制 WebSocket
- Codex 的预热连接可减少 50%+ 首次响应延迟
- WebChat 客户端可能因网络问题断开 WebSocket

**Go 实现方案**：
```
internal/transport/
  transport.go      # Transport 接口：Send/Recv/Close
  ws_transport.go   # WebSocket 实现
  sse_transport.go  # SSE 实现（HTTP streaming）
  fallback.go       # 自动回退逻辑：WS → SSE
  pool.go           # 预热连接池
```

- `Transport` 接口抽象传输层，Worker 和 Client 通过统一接口通信
- 回退策略：WS 握手失败 → 降级 SSE → 告知客户端
- 预热池：每个 Worker Type 保持 N 个空闲连接

**涉及模块**：
- 新建 `internal/transport/` 包
- `internal/gateway/conn.go` — 使用 Transport 接口
- `internal/worker/base/conn.go` — SessionConn 支持多传输
- `internal/gateway/handler.go` — 连接回退逻辑

---

#### 3.3 Thread Fork 与 Archive

**是什么**：
扩展 Session 模型，支持从任意历史点分叉出新会话（Fork），以及将长时间不活跃的会话归档而非删除（Archive）。

**为什么重要**：
- 已有 `ForkSession` 平台键，但只支持 Worker 级 fork
- 归档允许恢复旧会话而非丢失
- Codex 的 fork/archive 模式对长周期项目有实际价值

**Go 实现方案**：
```go
// internal/session/manager.go 扩展
func (m *Manager) Fork(ctx context.Context, sessionID string, forkPoint int64) (*SessionInfo, error)
func (m *Manager) Archive(ctx context.Context, sessionID string) error
func (m *Manager) Restore(ctx context.Context, sessionID string) (*SessionInfo, error)
```

- Fork: 创建新 Session，复制 forkPoint 之前的 eventstore 历史
- Archive: 将 Session 状态设为 `Archived`（新增状态），释放 Worker 资源
- Restore: 从 Archive 恢复，重新启动 Worker 并加载历史

**涉及模块**：
- `internal/session/manager.go` — 新增 Fork/Archive/Restore 方法
- `pkg/events/events.go` — 新增 `StateArchived` 状态
- `internal/eventstore/store.go` — 支持历史事件复制
- `internal/session/store.go` — 持久化 Archive 状态

---

#### 3.4 Go SDK 程序化访问

**是什么**：
将现有 `client/` 包升级为完整的 Go SDK，提供类型安全的会话管理、事件订阅、Worker 控制等能力，方便 Go 服务端程序集成 HotPlex。

**为什么重要**：
- 现有 `client/` 是基础 WebSocket 客户端
- Go 生态的企业需要以代码方式（非 YAML）定义 Agent 工作流
- 可以构建在 HotPlex 之上的高级编排框架

**Go 实现方案**：
```go
// client/sdk.go
type SDK struct {
    client   *Client
    sessions *SessionManager
    workers  *WorkerController
}

func NewSDK(opts ...SDKOption) (*SDK, error)

// SessionManager: 类型安全的会话 CRUD
type SessionManager struct {
    Create(ctx context.Context, opts SessionOpts) (*Session, error)
    Get(ctx context.Context, id string) (*Session, error)
    List(ctx context.Context, filter SessionFilter) ([]*Session, error)
    Stream(ctx context.Context, id string) (<-chan *events.Envelope, error)
}

// WorkerController: 程序化 Worker 管理
type WorkerController struct {
    ListWorkers(ctx context.Context) ([]WorkerInfo, error)
    Health(ctx context.Context, sessionID string) (*worker.WorkerHealth, error)
    Command(ctx context.Context, sessionID string, cmd WorkerStdioCommand) error
}
```

- 复用 `client.Client` 的 WebSocket 基础
- 添加重连、背压处理、类型反序列化
- 支持 context 取消和优雅关闭

**涉及模块**：
- `client/` — 扩展为完整 SDK
- `client/examples/` — 增加编排示例

---

#### 3.5 WebChat 多 Agent 面板

**是什么**：
升级 WebChat 界面，支持同时显示多个 Agent 的执行状态，实现多 Agent 并行执行的可视化。

**为什么重要**：
- Codex 的 TUI 多 Agent 面板在开发者社区反响良好
- 与 Phase 2 的 Session Federation 配合，提供可视化协作界面
- 热重载 WebChat 是可行的（Next.js SPA）

**Go 实现方案**：
- 后端：`internal/gateway/api.go` 扩展 `/api/sessions` 支持多 Session 并列查询
- 前端：WebChat 多列布局，每列一个 Session，共享输入框
- 实时状态：WebSocket 推送所有活跃 Session 的状态更新

**涉及模块**：
- `internal/gateway/api.go` — 多 Session API
- `internal/webchat/server.go` — 新增 API 路由
- `webchat/` — 前端多面板布局

---

#### 3.6 Code Mode（嵌套工具执行）

**是什么**：
允许 Agent 在执行工具调用时进入"代码模式"，直接执行嵌套的代码修改操作，而非返回等待用户确认。

**为什么重要**：
- Codex 的 Code Mode 显著提升了 Agent 的自主执行能力
- 复杂任务（如"重构整个模块"）需要连续多次工具调用
- 结合 Guardian 自动审批，可以实现安全的自主执行

**Go 实现方案**：
```go
// internal/gateway/bridge.go 扩展
// Code Mode: 在 forwardEvents 中追踪嵌套工具调用深度
type CodeMode struct {
    Active      bool
    Depth       int
    MaxDepth    int    // 默认 5
    ToolCount   int
    MaxTools    int    // 默认 20
    AutoCompact bool   // 嵌套执行时自动压缩上下文
}
```

- Code Mode 开启时，工具调用自动审批（与 Guardian 集成）
- 深度/次数限制防止无限递归
- 超限时退回交互模式

**涉及模块**：
- `internal/gateway/bridge.go` — Code Mode 状态机
- `internal/approval/guardian.go` — Code Mode 下的自动审批策略
- `pkg/events/events.go` — 新增 CodeMode 事件类型
- `internal/messaging/interaction.go` — Code Mode 期间暂停用户交互

---

## 四、优先级依赖关系图

```
Phase 1 (Q3 2026)                    Phase 2 (Q4 2026)
┌─────────────────┐                ┌──────────────────────┐
│ 1.1 Guardian    │──→ Phase 2.5   │ 2.1 Contributor 架构  │
│ 1.2 沙箱增强    │──→ Phase 3.6   │ 2.2 Memory 系统      │
│ 1.3 Hook 系统   │──→ Phase 2.1   │ 2.3 多 Agent 协作    │
│ 1.4 ToolExposure │──→ Phase 2.4   │ 2.4 Skill Marketplace │
│ 1.5 分布式追踪   │                │ 2.5 Auto-Compact      │
└─────────────────┘                │ 2.6 Token 预算管理    │
                                   └──────────────────────┘
                                           │
                                           ▼
                                   Phase 3 (Q1 2027)
                                   ┌──────────────────────┐
                                   │ 3.1 Agent 路由       │
                                   │ 3.2 传输回退          │
                                   │ 3.3 Thread Fork/Arch │
                                   │ 3.4 Go SDK           │
                                   │ 3.5 多 Agent 面板     │
                                   │ 3.6 Code Mode        │
                                   └──────────────────────┘
```

**关键路径**：
- Guardian (1.1) → Auto-Compact (2.5) → Code Mode (3.6) — 自主执行链
- Hook (1.3) → Contributor (2.1) → Memory (2.2) → Agent 路由 (3.1) — 智能化链
- ToolExposure (1.4) → Skill Marketplace (2.4) — 生态链

---

## 五、Go 实现通用原则

1. **接口优先**：所有扩展点通过 Go interface 定义，与 Worker Registry、Platform Adapter 同一模式
2. **init() 自注册**：新模块通过 `init()` + blank import 自注册，`main.go` 只需加 `_ import`
3. **Build Tags 隔离平台**：Linux 专用功能（Landlock/seccomp）通过 `//go:build linux` 隔离
4. **热重载友好**：新功能支持热重载（Observer 模式 + atomic.Pointer），避免重启
5. **向后兼容**：新增字段通过 `omitempty` JSON tag 保持协议兼容
6. **测试先行**：每个新模块包含 `*_test.go`，复用现有的 mock 和 testutil 模式
7. **AGENTS.md 同步更新**：每个新包创建 `AGENTS.md` 文档，保持代码可导航性

---

## 六、风险评估与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| Linux 沙箱在容器环境中不兼容 | 中 | 检测 cgroup/root 权限，容器内退化为路径限制 |
| Guardian 自动审批导致误操作 | 高 | fail-closed + 审计日志 + 人工回滚 |
| 多 Agent 协作增加调试复杂度 | 中 | 完善分布式追踪 + 可视化面板 |
| Skill Marketplace 安全风险 | 高 | 签名验证 + 沙箱隔离 + 审核流程 |
| Go Plugin 模式稳定性 | 低 | Phase 2 先用接口模式，Phase 3 考虑 Go Plugin |

---

## 七、成功指标（KPI）

### Q3 2026
- [ ] Guardian 覆盖 100% Cron 任务的权限请求
- [ ] Worker 进程沙箱在 Linux/macOS 上启用率 > 80%
- [ ] Hook 系统支持 10 种标准事件类型
- [ ] 分布式追踪覆盖率 > 95%（客户端 → Gateway → Worker）

### Q4 2026
- [ ] Contributor 架构支持至少 5 种标准扩展点
- [ ] Memory 系统支持 3 种命名空间隔离
- [ ] 多 Agent 协作支持 DAG 定义和 fan-out/fan-in
- [ ] Skill Marketplace 支持 Git 仓库安装

### Q1 2027
- [ ] Agent 路由准确率 > 85%（基于历史任务分析）
- [ ] Go SDK 支持 100% Gateway API
- [ ] WebChat 支持多 Agent 并行可视化
- [ ] Code Mode 安全运行率 > 99.9%（零误操作）

---

*文档版本：v1.0*
*创建时间：2026-06-12*
*基于 HotPlex v1.28.0 代码库分析*
