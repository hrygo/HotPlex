# HotPlex 核心架构设计文档

*查看其他语言: [English](architecture.md), [简体中文](architecture_zh.md).*

<div align="center">
  <img src="images/hotplex_beaver_banner.webp" alt="HotPlex 横幅"/>
</div>


HotPlex 是一个高性能的 **AI 智能体运行时 (Agent Runtime)**，旨在将原本"单次运行"的 AI CLI 工具（如 Claude Code, OpenCode）转化为生产就绪的长生命周期交互服务。它的核心哲学是"利用胜于构建 (Leverage vs Build)"，通过维护持久化的、具备安全围栏的全双工进程池，彻底消灭 Headless 模式下的冷启动代价，实现毫秒级的指令响应。

**版本**: v0.17.0 | **核心角色**: AI 智能体工程协议 (Cli-as-a-Service)

---

## 1. 物理布局与清洁架构 (Physical Layout)

HotPlex 遵循分层架构与严格的可见性规则，将公开 SDK 与内部执行细节、协议适配器完全隔离。

### 1.1 目录结构（实测）

- **根目录 (`/`)**：SDK 的主要入口。包含 `hotplex.go` (公开别名) 以及 `client.go`。
- **`engine/`**：公开的执行运行器 (`Engine`)。编排 Prompt 执行、安全 WAF 审计及事件分发。
- **`provider/`**：不同 AI CLI 智能体的抽象层。包含 `Provider` 接口及 `claude-code`、`opencode` 的具体实现。
- **`types/`**：基础数据结构 (`Config`, `StreamMessage`, `UsageStats`)。
- **`event/`**：统一事件协议与回调定义 (`Callback`, `EventWithMeta`)。
- **`chatapps/`**：**平台接入层**。将 HotPlex 连接到社交平台，支持多适配器架构：
  - **Slack 适配器** (`chatapps/slack/`): Block Kit UI、Socket Mode、原生流式输出、Assistant Status
  - **飞书适配器** (`chatapps/feishu/`):  Lark/飞书自定义机器人
  - 核心组件：
    - `engine_handler.go`：将平台消息桥接为引擎指令
    - `manager.go`：机器人适配器的生命周期管理
    - `processor_*.go`：消息格式化、频率限制、线程管理、分块等处理器链
    - `base/adapter.go`：带会话管理的抽象基类适配器
- **`internal/engine/`**：核心执行引擎。管理 `SessionPool` (线程安全的进程复用) 与 `Session` (I/O 管道与状态管理)。
- **`internal/persistence/`**：**会话持久化**。管理 `SessionMarkerStore`，用于在系统重启后检测并恢复持久化 CLI 会话。
- **`internal/security/`**：基于正则的指令级 WAF (`Detector`)，以及 `danger_block` 闭环安全确认。
- **`internal/sys/`**：**安全原语**。提供跨平台的进程组 ID (PGID) 管理与信号级联处理的底层实现。
- **`internal/server/`**：协议适配层。包含 `hotplex_ws.go` (WebSocket) 与 `opencode_http.go` (REST/SSE)。
- **`internal/config/`**：基于文件监视器的配置热重载。
- **`internal/strutil/`**：高性能字符串处理与路径清洗工具。

### 1.2 设计原则

1.  **公开层薄，私有层厚**：根包 `hotplex` 仅提供最小且稳定的 API 表面。
2.  **策略模式 (Provider)**：将引擎与特定 AI 工具解耦。`provider.Provider` 允许在不改变执行逻辑的情况下切换后端。
3.  **PGID 优先安全策略**：安全并非事后补救；每一次执行都封装在独立的进程组中，以防止孤儿进程泄漏。
4.  **IO 驱动状态机**：`internal/engine` 使用 IO 信号标记而非固定延时来管理进程状态（启动中、就绪、忙碌、死亡）。
5.  **SDK 优先**：所有平台集成使用官方 SDK (slack-go 等)，禁止手动实现协议。

---

## 2. 核心系统组件

### 2.1 引擎运行器 (`engine/runner.go`)
*   **Engine 单例**：用户的主要接口 (`NewEngine`, `Execute`)。
*   **安全注入**：动态将全局 `EngineOptions`（如 `AllowedTools`）注入下游会话。
*   **确定性会话 ID**：使用 UUID v5 将业务对话 ID 映射为持久会话，确保高上下文缓存命中率。

### 2.2 适配层 (`provider/`)
将多样的 CLI 协议标准化为统一的 "HotPlex 事件流"：
*   **Provider 接口**：处理 CLI 参数构建、输入负载格式化与事件解析。
*   **工厂与注册表**：`ProviderFactory` 管理实例创建，`ProviderRegistry` 缓存活跃实例以供复用。
*   **支持的 Provider**：Claude Code (默认)、OpenCode。

### 2.3 会话管理器 (`internal/engine/pool.go`)
*   **热连结 (Hot-Multiplexing)**：`SessionPool` 维护活跃进程表。对同一 SessionID 的重复请求将跳过"冷启动"直接进行"热执行"。
*   **慢路径保护 (Slow Path Guard)**：会话访问是线程安全的；针对同一 ID 的并发冷启动请求将通过 `pending` 状态映射进行排队，防止重复 fork 进程。
*   **优雅 GC**：使用 `cleanupLoop` 根据 `IdleTimeout` 定期清理空闲进程。

### 2.4 安全与系统隔离 (`internal/security/`, `internal/sys/`)
为了防止智能体产生后台任务导致"僵尸"进程，HotPlex 强制执行进程组级隔离：
*   **Unix**: 在 `fork()` 时调用 `setpgid(2)`。终止时向负值 PID（如 `kill -PID`）发送 `SIGKILL`，清除整个进程树。
*   **Windows**: 使用 **Job Objects** (`CreateJobObjectW`)。添加 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 标志，确保主 Job 句柄关闭时，OS 保证终止所有关联的子进程。
*   **WAF 审计**: 在指令到达 Provider 之前，扫描输入 Prompt 是否包含恶意命令字符串（如 `rm -rf /`）。
*   **Danger Block 闭环**: 交互式安全确认块（`danger_block` 事件），包含用户批准工作流。

### 2.5 ChatApps 平台适配器 (`chatapps/`)
多平台支持，架构一致：

| 平台 | 协议 | 关键特性 |
|------|------|----------|
| **Slack** | Socket Mode + Web API | Block Kit、原生流式、Assistant Status、斜杠命令 |
| **飞书** | 自定义机器人 | 卡片消息、交互回调 |

#### 核心接口
- **ChatAdapter**: 所有平台适配器的基接口
- **MessageOperations**: 平台特定的消息操作（删除、更新、流式）
- **SessionOperations**: 会话查询与管理
- **StatusProvider**: AI 状态通知抽象
- **StreamWriter**: 平台无关的流式接口

### 2.6 事件钩子与可观测性 (`hooks/`, `telemetry/`)
*   **Webhooks 与审计**: 旁路异步向外部系统 (Slack, Webhooks) 广播负载事件，不阻断核心热执行链路。
*   **追踪与指标**: 推送原生 OpenTelemetry 分布式追踪，并暴露 `/metrics` 接口供 Prometheus 采集。

---

## 3. 会话生命周期与数据流

```mermaid
sequenceDiagram
    participant Social as "Slack / 飞书"
    participant ChatApps as "chatapps.Adapter"
    participant Client as "客户端 (WebSocket/SDK)"
    participant Server as "internal/server"
    participant Engine as "engine.Engine"
    participant Security as "internal/security (WAF)"
    participant Storage as "internal/persistence"
    participant Pool as "internal/engine.SessionPool"
    participant Provider as "provider.Provider"
    participant Proc as "CLI 进程 (OS)"

    Note over Social AI 机器人集成, ChatApps:链路
    Social->>ChatApps: Webhook / Socket 事件
    ChatApps->>Engine: Execute(Config, Prompt)

    Note over Client, Server: 直接 API 链路
    Client->>Server: 发送请求 (WS / POST)
    Server->>Engine: Execute(Config, Prompt)

    Engine->>Security: WAF 检查 (Detector)
    alt 被 WAF 拦截
        Engine-->>ChatApps: danger_block 事件
        ChatApps-->>Social: 交互式确认块
        Social->>ChatApps: 用户批准
        ChatApps->>Engine: 带批准重试
    end

    Engine->>Hooks: 启动 Trace Span
    Engine->>Pool: 获取或创建会话 (ID)

    alt 冷启动 (不在池中)
        Pool->>Storage: 检查标记 (Resume?)
        Pool->>Provider: 构建 CLI 参数/环境变量
        Pool->>Proc: fork() 并分配 PGID / Job Object
        Pool->>Proc: 注入上下文/系统提示词
    end

    Engine->>Proc: 写入 stdin (JSON 负载)

    loop 流式归一化
        Proc-->>Provider: 原始工具特定输出
        Provider-->>Engine: 归一化 ProviderEvent

        alt 路由回 ChatApps
            Engine-->>ChatApps: 回调事件
            ChatApps-->>Social: 原生流式 / Assistant Status
        else 路由回 API
            Engine-->>Server: 公开 EventWithMeta
            Server-->>Client: WebSocket/SSE 事件
        end
    end

    Engine->>Pool: Touch(ID) 更新心跳
    Engine->>Hooks: 结束 Trace 并写入指标
```

---

## 4. 功能矩阵

### 核心能力
- [x] 具备 `internal/` 隔离的清洁架构
- [x] 基于策略模式的 Provider 机制 (Claude Code, OpenCode)
- [x] 弹性的会话热复用 (Hot-Multiplexing)
- [x] 跨平台 PGID 管理 (Windows Job Objects / Unix PGID)
- [x] 基于正则的安全 WAF 及 Danger Block 闭环
- [x] **双协议网关**：原生 WebSocket 与 OpenCode 兼容的 REST/SSE API
- [x] **多平台适配器**：Slack、飞书
- [x] **Slack 原生特性**：Block Kit UI、流式输出、Assistant Status、斜杠命令
- [x] **事件钩子 (Event Hooks)**：支持 Webhook 及多种自定义审计通知接收器
- [x] **可观测性**：原生集成 OpenTelemetry 追踪与 Prometheus 性能指标 (`/metrics`)
- [x] **配置热重载**：基于 YAML 的配置与文件监视器

### 平台特性对比

| 特性 | Slack | 飞书 |
|------|-------|------|
| Block Kit UI | ✅ | - |
| 原生流式 | ✅ | - |
| Assistant Status | ✅ | - |
| 斜杠命令 | ✅ | - |
| 签名验证 | ✅ | ✅ |
| 交互按钮 | ✅ | Card |

### 规划增强
- **L2/L3 级隔离**：集成 Linux Namespace (PID/Net) 与 WASM 沙箱
- **多智能体总线**：在单一命名空间下编排多个专业化智能体

---

<div align="center">
  <img src="images/logo.svg" alt="HotPlex 图标" width="100"/>
  <br/>
  <sub>为 AI 工程化社区倾力构建</sub>
</div>

