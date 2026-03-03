# 🌟 战略规划：HotPlex 原生 LLM 全局大脑 (Native Brain) 架构设计

## 1. 背景与目标分析 (Goal Description)

当前 HotPlex 作为连接多种 ChatApps (Slack, Telegram, DingTalk 等) 与后端 AI Agent (Engine Provider) 的 **Agentic Craw Layer**。由于外部的 Engine Provider 通常负责繁重的业务逻辑推理与工具调用，不可避免地会带来高延迟、高开销。

为了实现真正的智能化平台中枢，HotPlex 需要在独立于 Engine Provider 之外，引入一个 **轻量级、原生支持的全局 LLM 大脑 (Native Brain)**。它的核心职责是处理平台级的、全局性的智能化需求，将确定性逻辑与轻量级推理结合，起到“智能网关”和“平台总控”的作用。

---

## 2. 角色定位：Native Brain vs Engine Provider

为了避免职责混淆，首先需要明确这两者的边界：

| 维度         | HotPlex Native Brain (全局大脑)                          | 独立 Engine Provider (外部代理)                           |
| :----------- | :------------------------------------------------------- | :-------------------------------------------------------- |
| **核心定位** | 智能网关、调度中枢、上下文管理者                         | 领域专家、业务执行者、工作流执行者                        |
| **主要任务** | 意图识别、智能路由、消息过滤、上下文压缩隔离、全局风控   | 深度推理、业务数据分析、调用外部业务 API 的复杂逻辑       |
| **处理位置** | 处于 ChatApp 适配器与 Engine 层之间 (HotPlex 框架层)     | 独立运行的工作节点或外部隔离环境 (如 CLI 引擎)            |
| **性能要求** | 高频次、超低延迟、低成本模型为主 (如 Flash / Haiku 级别) | 低频次、高延迟可接受、高智能模型为主 (如 Pro / Opus 级别) |

---

## 3. 全局智能化需求场景 (Proposed Core Capabilities)

接入原生 LLM 大脑后，将为 HotPlex 解锁以下全局智能化能力：

### 1) 意图识别与群组噪音过滤 (Intent Pre-processing)
* 在 Slack 或群组场景中，通过 Native LLM 快速判断用户发言是否在对 Bot 说话（尤其是没有 @ 的情况下）。
* 判断意图是否属于“闲聊”、“指令”还是“复杂任务”，对无需调用重型 Engine Provider 的消息直接给予快速响应（降级机制）。

### 2) 智能路由与多代理分发 (Intelligent Routing)
* 读取用户的请求后，由全局大脑判断请求应该分配给哪个特定的 Engine Provider 实例或系统内注册的不同 Agent（例如：将技术问题路由给 Codegen Agent，报销问题路由给 HR Agent）。

### 3) 上下文压缩与长文本管理 (Context Summarization & Compression)
* 平台级的记忆管理：当 ChatApp 的会话历史超过 Engine Provider 的上下文窗口时，由 Native Brain 负责对历史记录进行无损摘要压缩，生成核心 Context 传递给底层 Engine。
* 并发会话的状态清理与摘要固化。

### 4) 全局安全与风控网关 (Safety & Guardrails)
* **输入风控**：拦截 Prompt 注入 (Prompt Injection) 或恶意指令，拒绝后直接返回告警，不透传给 Engine 层。
* **输出风控**：对 Engine 返回的敏感数据 (如内部 IP、机密 Token) 自动进行脱敏处理。

### 5) "Chat2Config" - 平台自驱配置 (Operational Intelligence)
* 允许管理员通过自然语言对话，自动变更 HotPlex 的通道配置、权限策略或流控参数，无需手动修改 YAML。

### 6) 视觉推理透视 (Visual Reasoning Chunks)
* **需求场景**：在等待 Engine 响应的漫长过程中（如正在扫描大型项目），Native Brain 监听 Engine 的中间状态事件，并将其转化为“人类可理解”的微光文字。
* **具体体现**：在 Slack 的 `AssistantStatus` 中实时展示：“正在从 12 个文件中检索上下文...”、“关联历史会话主题：API 认证重构...”、“正在计算最佳工具链...”。
* **价值**：极大缓解用户的等待焦虑，提供更透明、更具“智力感”的交互体验。

---

## 4. 架构设计与重构建议 (Proposed Changes)

---

## 4. 架构设计与深层集成 (Architecture & Deep Integration)

基于对 HotPlex 源码的深度调研 (`hotplex/engine`, `hotplex/hooks`, `intengine.TurnState`)，我们将在 HotPlex 根目录下新增 `brain` 模块，作为与 `engine`、`chatapps` 平行的一等公民。

### 核心目录结构规划
```diff
  chatapps/
  engine/           # 原有的 Engine Provider 对接层 (处理进程热复用)
  hooks/            # 原有的事件广播层
+ brain/            # [NEW] 全局原生 LLM 大脑核心包
+   router.go       # 负责意图识别与多代理智能分发 (可注册多个 Engine ID)
+   memory.go       # 负责对接 intengine.TurnState 进行长上下文的无损压缩与摘要持久化
+   guard.go        # 接入 hooks 架构，拦截 danger.blocked 等核心事件进行全局的安全隔离与输出过滤
+   llm/            # 轻量化、统一的内部大模型适配层 (如 gcp-gemini, anthropic 或 openai API)
```

### 关键集成点 (Integration Hooks)
1. **ChatApp 层入口拦截 (Pre-processing)**：
   - 在 `chatapps/engine_handler.go` 接收到 `ActionMessage` 或原始文本时，先通过 `brain.IsRelevant(msg)` 判断是否需要转发。对于无需深度推理的闲聊，由 Brain 直接生成响应，实现**毫秒级低延迟回包**。
2. **状态管理层集成 (Memory Compression)**：
   - 监听 `intengine` 的 `TurnState` 及 `SessionStats`。当 `TotalTokens` 接近设定的阈值 (如 8K/16K) 时，触发 `brain.CompressHistory(ctx, sessionID)`，将早期的回合制消息进行摘要重构，释放 Token 空间。
3. **事件总线集成 (Self-healing & Operational Intel)**：
   - 将 Native Brain 注册为一个高级的 Event Hook (`hooks.EventTurnEnd`, `hooks.EventSessionError`)。
   - 当 Engine Provider 频繁抛出 `tool.error` 时，Brain 检测并尝试修复 Prompt 发送系统自愈指令。

---

## 5. HotPlex 的能力跃迁 (Capability Leaps)

原生的全局大脑不只是一层过滤网，也是驱动 HotPlex 进化到智力基础设施的核心。

1. **动态网络架构：多代理路由中枢 (Multi-Agent Router)**
   - 突破单点模型限制：面对复杂指令（例如：“帮我分析日志并修复 bug”），Brain 能够将任务拆解，分别路由给 *LogAnalysis Agent* 和 *CodeFix Agent*（独立进程），最后对两个 Engine 提供的数据进行合并与总结。
2. **极速交互体验与成本节约 (Cost & Latency Optimization)**
   - 通过意图识别层，对占日常请求比重 40% 的非工具化查询拦截处理（使用 Flash 级别极低成本模型），有效降低后端重型模型（如 Claude Opus / Pro）的 Token 开支和响应延迟。
3. **自动化的“Chat2Config”系统**
   - 给平台管理员赋能。允许管理员直接在 Slack/Discord 对 Bot 喊话：“开启安全拦截模式”、“设置当前通道的默认模型为 Opus”，Brain 理解后自动解析修改宿主机 YAML 或运行内存 Config，实现完全自然语言的系统管控。
  ```yaml
  brain:
    enabled: true
    provider: "openai" # 或 gemini/anthropic
    model: "gpt-4o-mini" # 强调使用快速模型
    api_key: "${BRAIN_API_KEY}"
    features:
      - intent_routing
      - context_compression
      - safety_guard
  ```

### [MODIFY] `chatapps/engine_handler.go` 增强
修改当前的 `StreamCallback` 和消息分发机制：
1. **拦截点 (Interceptors)**：在 Adapter 收到用户消息、准备转发给 Engine 之前，增加一个 `Brain.PreProcess(ctx, msg)` 的 Hook，用于执行群聊过滤和意图识别。
2. **后处理点 (Post-Processors)**：在 Engine 返回消息准备发给 Adapter 之前，插入 `Brain.PostProcess(ctx, msg)`，用于输出风控与总结。

---

## 5. 实施路径 (Iterative Rollout & Verification Plan)

### 第一阶段：基础设施建设 (Foundation)
* **目标**：建立 `brain` 包基础架构，能够独立于 Engine Provider 直接与基础大模型（通过系统级 API_KEY）进行无状态对话交互。
* **验证验证**：编写单元测试检查 `brain.Ask(ctx, prompt)` 是否能正确使用系统级 Token 建立快速响应。

### 第二阶段：意图拦截与快速降级 (Pre-processing)
* **目标**：在群组（如 Slack/Telegram）中接入原生大脑。收到未直接 @Bot 的含糊消息时，让 Brain 判断相关性。
* **验证验证**：在 Slack 中测试无关闲聊，确认 Native Brain 能拦截请求不再下发给 Engine Provider，从而降低系统成本。

### 第三阶段：上下文压缩 (Context Compression)
* **目标**：接入 `intengine.TurnState` 之前，如果发现上下文 Token 超限，由 Brain 自动压缩早期对话。
* **验证验证**：注入10万 Token 的模拟对话历史，观察 Brain 是否能在 2 秒内将其压缩为 2 千词的高质量摘要。

---

