*查看其他语言: [English](README.md), [简体中文](README_zh.md).*

# HotPlex 文档索引

欢迎使用 HotPlex 文档。本目录包含了为开发者、架构师和用户准备的 HotPlex 控制平面完整指南。

## 🏗️ 核心概念
- **[架构概览](architecture_zh.md)**: 高层系统设计，安全模型（PGID 隔离）以及性能原则。
- **[SDK 指南](sdk-guide_zh.md)**: 如何将 HotPlex 集成到您的 Go 应用程序中。
- **[快速开始](quick-start_zh.md)**: HotPlex 快速入门教程。

## 🚀 部署与运维
- **[可观测性指南](observability-guide_zh.md)**: OpenTelemetry 追踪与 Prometheus 指标集成。
- **[Docker 部署](docker-deployment_zh.md)**: 容器与 Kubernetes 部署指南。
- **[生产环境指南](production-guide_zh.md)**: 生产环境部署最佳实践。
- **[基准测试报告](benchmark-report_zh.md)**: HotPlex 性能表现分析。

## 🖥️ 服务端模式 (智能体控制平面)
用于与处于服务模式下的 HotPlex 进行交互的开发者指南（WebSocket & OpenCode 协议）。
- **[服务端 API 手册](server/api_zh.md)**: 详细的协议流程、请求/事件架构及多语言示例。

## 🤖 AI 提供商集成
HotPlex 支持的特定 AI CLI 智能体深度指南。
- **[Claude Code 提供商](providers/claudecode_zh.md)**: 与 Anthropic 的 Claude Code CLI 集成。
- **[OpenCode 提供商](providers/opencode_zh.md)**: 与 OpenCode CLI 生态系统集成。

## 💬 ChatApps 集成
- **[ChatApps 架构](chatapps/chatapps-architecture.md)**: 架构设计与平台适配器模式。
- **[Slack 适配器手册](chatapps/chatapps-slack-manual.md)**: Slack 集成完整指南（2026 AI 原生版）。
- **[Slack 架构深度解析](chatapps/chatapps-slack-architecture.md)**: 技术架构分析。
- **[钉钉集成分析](chatapps/chatapps-dingtalk-analysis.md)**: 钉钉适配器分析与设计。
- **[飞书手册](chatapps/chatapps-feishu-manual.md)**: 飞书机器人集成指南。
- **[飞书生产检查清单](chatapps/chatapps-feishu-production-checklist.md)**: 飞书部署检查清单。
- **[Slack AI 原生演进](plans/slack-ai-native-evolution-plan.md)**: 下一代 Slack AI 原生交互体验。

## 🔐 安全与扩展
- **[Hooks 架构](hooks-architecture_zh.md)**: 事件钩子系统，用于扩展性。

## 📋 设计实现记录
- **[原生大脑架构](plans/native-brain-architecture_zh.md)**: AI 原生 UX 架构设计。
- **[消息存储设计](plans/chatapp-message-storage-design.md)**: 消息持久化设计。
- **[存储插件设计](plans/hotplex-storage-plugin-design.md)**: 存储扩展性设计。
- **[Slack Channel Workdir 设计](plans/2026-03-03-slack-channel-workdir-design.md)**: 按频道工作目录设计。

---

*最近更新: 2026-03-04*
*版本: v0.17.0*
