*Read this in other languages: [English](README.md), [简体中文](README_zh.md).*

# HotPlex Documentation Index

Welcome to the HotPlex documentation. This directory contains comprehensive guides for developers, architects, and users of the HotPlex control plane.

## 🏗️ Core Concepts
- **[Architecture Overview](architecture.md)**: High-level system design, security model (PGID isolation), and performance principles.
- **[SDK Guide](sdk-guide.md)**: How to integrate HotPlex into your Go applications.
- **[Quick Start](quick-start.md)**: Step-by-step tutorial for getting started with HotPlex.

## 🚀 Deployment & Operations
- **[Observability Guide](observability-guide.md)**: OpenTelemetry tracing and Prometheus metrics integration.
- **[Docker Deployment](docker-deployment.md)**: Container and Kubernetes deployment guide.
- **[Production Guide](production-guide.md)**: Production deployment best practices.
- **[Benchmark Report](benchmark-report.md)**: Detailed performance metrics and analysis.

## 🖥️ Server Mode (Agent Control Plane)
Developer guides for interacting with HotPlex in server mode (WebSocket & OpenCode protocols).
- **[Server API Manual](server/api.md)**: Detailed protocol flow, request/event schemas, and multi-language examples.

## 🤖 AI Provider Integrations
Deep-dive guides for specific AI CLI agents supported by HotPlex.
- **[Claude Code Provider](providers/claudecode.md)**: Integration with Anthropic's Claude Code CLI.
- **[OpenCode Provider](providers/opencode.md)**: Integration with the OpenCode CLI ecosystem.

## 💬 ChatApps Integration
- **[ChatApps Architecture](chatapps/chatapps-architecture.md)**: Architecture design and platform adapter patterns.
- **[Slack Adapter Manual](chatapps/chatapps-slack-manual.md)**: Comprehensive Slack integration guide (2026 AI-Native Edition).
- **[Slack Architecture Deep Dive](chatapps/chatapps-slack-architecture.md)**: Technical architecture analysis.
- **[DingTalk Integration](chatapps/chatapps-dingtalk-analysis.md)**: DingTalk adapter analysis and design.
- **[Feishu (飞书) Manual](chatapps/chatapps-feishu-manual.md)**: Feishu bot integration guide.
- **[Feishu Production Checklist](chatapps/chatapps-feishu-production-checklist.md)**: Deployment checklist for Feishu.
- **[Slack AI-Native Evolution](plans/slack-ai-native-evolution-plan.md)**: Next-generation Slack AI native experience.

## 🔐 Security
- **[Hooks Architecture](hooks-architecture.md)**: Event hooks system for extensibility.

## 📋 Design & Implementation Records
- **[Native Brain Architecture](plans/native-brain-architecture_zh.md)**: AI native UX architecture design.
- **[Session Storage Design](plans/chatapp-message-storage-design.md)**: Message persistence design.
- **[HotPlex Storage Plugin](plans/hotplex-storage-plugin-design.md)**: Storage extensibility design.
- **[Slack Channel Workdir Design](plans/2026-03-03-slack-channel-workdir-design.md)**: Per-channel working directory design.

---

*Last Updated: 2026-03-04*
*Version: v0.17.0*
