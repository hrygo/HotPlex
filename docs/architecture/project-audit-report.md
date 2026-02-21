# 🚀 hotplex 项目顶级化改进建议报告 (V2.1)

## 0. 综合现状评估
**hotplex** 目前已经具备了极佳的基础：清晰的“第一性原理”（Leverage vs Build）、生产级的品牌定位、以及解决 AI Agent 领域核心痛点（冷启动与安全）的洞察。

然而，要真正站稳 AI 基础设施的生态位，项目必须从一个优秀的“代理拦截器”进化为**“Agent 运行时（Runtime）”**。这意味着需要从**抽象层级、安全性边界、可扩展性、以及生产级可观测性**上进行全方位跨越。本报告融合了 2025 年最新调研与插件化设计思想，旨在锚定项目的顶尖开源坐标。

---

## 1. 架构升级：从“封装者”进化为“运行时平台”
目前 hotplex 与特定的 CLI 工具绑定较深，且配置灵活性有待增强。

*   **建议 A：引入 Provider 接口抽象层**
    *   **实现**：定义标准接口 `Provider`，支持 `ClaudeCodeProvider`, `AiderProvider`, `OpenCodeProvider`。用户可通过配置动态切换底座。
*   **建议 B：协议归一化与事件解耦 (Normalize IO)**
    *   **改进**：定义 HotPlex 专有的事件模型（Events V1），借鉴 LSP 或 CloudEvents 标准。将不同工具的输出 normalize 为统一格式，实现“Agent 换底座，SDK/UI 不用改”。
*   **建议 C：分层配置引擎 (Tiered Configuration)**
    *   **改进**：引入类似 Git 的配置模型（System > Global > Task-level）。支持在 Engine 初始化时设置全局底色，并允许在单次 Execute 时通过 Config 动态覆盖特定参数。
*   **建议 D：确定性状态机 (Deterministic FSM)**
    *   **改进**：彻底舍弃 `WriteInput` 中的硬编码延时。改用 **IO 信号驱动**，通过解析 stdout 中的特定标记（如 `result` 消息或状态 JSON）实时驱动状态迁移，实现极致响应。

---

## 2. 安全性与可靠性：级联隔离与“零确认”沙盒
*   **建议 A：语义审核与“草稿模式 (Draft Mode)”**
    *   **改进**：接入小型 Local LLM (如 Llama-Guard) 对高风险命令进行实时意图审核。对于高风险操作，引擎暂停执行并抛出 `await_approval` 事件，支持“人机协同确认”。
*   **建议 B：级联隔离体系 (Multi-layer Isolation)**
    *   **L1 (Soft)**: 当前的 PGID 进程组管理。
    *   **L2 (Kernel)**: 集成 Linux Namespace (PID, Mount, Net) 与 Cgroups，限制单 Session 资源消耗。
    *   **L3 (Hard)**: 支持 MicroVM (如 Firecracker) 或 WASM 运行时，用于极高风险环境。
*   **建议 C：会话检查点与快照回滚 (Rollback)**
    *   **改进**：在执行重大操作前，利用底层文件系统技术（如 OverlayFS）创建 WorkDir 快照。如果 WAF 拦截或 Agent 失误，支持 `Rollback()` API 一键恢复现场。

---

## 3. 可扩展性：插件化生态 (Plugin System)
*   **建议 A：事件钩子插件 (Event Hooks)**
    *   **改进**：允许通过接口注册自定义插件。例如：`AuditPlugin`（审计所有敏感输出）、`NotifyPlugin`（执行完毕同步到 Slack）。
*   **建议 B：基于 WASM 的跨语言扩展**
    *   **改进**：长远看，引入 WASM 运行时作为插件加载器（类似 Envoy），允许用户使用任何语言编写 hotplex 的自定义安全规则或数据转换逻辑。

---

## 4. 生产级可观测性与 DX
*   **建议 A：原生集成 OpenTelemetry (OTel)**
    *   **改进**：集成 OTel Trace。开发者可量化：`Wait Queue -> Process Wakeup -> CLI Startup -> Tool Interaction -> Result Streaming` 的毫秒级分布。
*   **建议 B：成本审计与熔断 (Credit & Budgeting)**
    *   **改进**：实时计算 Token 成本 (USD)，支持针对 Session 设置“成本阈值熔断”，防止 Agent 行为失控导致高额账单。
*   **建议 C：可视化 Dashboard (hotplex-ui)**
    *   **改进**：提供开箱即用的 Web 管理面板，监控活跃会话、资源配额及安全拦截日志。

---

## 5. 智能化增强
*   **建议 A：上下文压缩与智能缓存**
    *   **改进**：自动感知会话长度。当上下文过长时，调用总结工具并利用 Token 缓存技术（如 Anthropic Prompt Caching）降低重复输入成本。
*   **建议 B：多 Agent 总线 (Internal Agent Bus)**
    *   **改进**：支持管理“进程组”。hotplex 负责在不同专业 Agent（如一个负责编码，一个负责审计）间进行语义路由。

---

## 6. 工程化与开源生态 (OSS Health)
*   **建议 A：工程质量补全**：建立 80%+ 的覆盖率，尤其是并发竞态测试。
*   **建议 B：自动化 Benchmarks**：量化 hotplex 相比原生 CLI 调用的性能提升百分比。
*   **建议 C：云原生部署**：提供 Docker-compose 一键拉起环境及 Helm Chart 支持。

---

## 7. 推荐改进路线图 (Consolidated Roadmap)

| 阶段                        | 重点项                                                   | 核心价值                                 |
| :-------------------------- | :------------------------------------------------------- | :--------------------------------------- |
| **Phase 1 (Stabilization)** | 补全并发单元测试、消除硬编码延时、L1 稳定性增强          | 达到生产级稳定与极致 Cold Start 体验     |
| **Phase 2 (Abstraction)**   | **Provider 接口化** + **分层配置系统** + **协议归一化**  | 兼容多工具（Aider 等），支持多级策略覆盖 |
| **Phase 3 (Enterprise)**    | **L2 内核隔离** + **语义 WAF** + **Draft 审批流**        | 建立企业级安全信任边界                   |
| **Phase 4 (Ecosystem)**     | **插件系统 (Hooks/WASM)** + **OTel** + **多 Agent 总线** | 形成完整的 Agent 运行时生态位            |
