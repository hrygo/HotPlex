# Draft: Hotplex Product Evolution Roadmap

## Requirements (confirmed)
- **Goal**: 分析并规划 Hotplex (AI Agent Control Plane) 的产品演进路线，形成详细的架构演进与功能迭代方案。
- **Product Identity**: 基于 Go 1.24 的高性能 AI Agent 控制平面，核心解决 CLI Agent（如 Claude Code, OpenCode 等）在生产环境中的“冷启动”延迟、安全阻断（Regex WAF）、会话状态（SessionPool）和流式通信（WebSocket）问题。

## Technical Decisions
- [等待用户进一步明确演进侧重点]

## Research Findings
- *Agent 1 (Explore)*: 正在后台扫描当前 codebase 的技术债务、硬编码限制（特别是 `pool.go` 和 `detector.go`），寻找可用性与性能瓶颈。
- *Agent 2 (Librarian)*: 正在后台调研业界 SOTA 竞品（如 E2B, Daytona, MCP 协议等）的企业级核心特性，提取演进参考指标。
- *Agent 3 (Oracle)*: 正在后台对当前的单机进程/Regex拦截架构进行深度剖析，推演向云原生/分布式沙箱架构演进的路线图。

## Open Questions
- 核心发力点与优先级（安全隔离 vs 多租户调度 vs 协议标准化）。
- 目标受众定义（私有化企业级部署 vs 公有云 SaaS 底座）。

## Scope Boundaries
- **INCLUDE**: Core Engine 架构升级、Provider 生态拓展、安全拦截层（Security/WAF）演进、会话调度层改造。
- **EXCLUDE**: Agent 自身的模型推理逻辑（Hotplex 专注于 Control Plane 职责）。
