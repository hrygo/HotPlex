# 🐳 HotPlex Docker 生态系统 (2026)

HotPlex 提供了针对 AI Agent 开发优化的全技术栈容器化环境。通过**延迟注入架构**与**多运行时基础层**，实现了极速的开发反馈循环与跨语言工具支持。

---

## 🏗️ 核心架构：延迟注入 (Late-Injection)

为了缩短“代码到容器”的反馈循环，我们将稳定的语言环境（SDK）与易变的应用程序二进制文件 (`hotplexd`) 解耦。

```text
    [ 官方 SDK 镜像 ]           [ HotPlex 源码 ]
     (Node, Python, Java)                │
               │                         ▼
               │                ┌───────────────────┐
               │                │ Dockerfile.artif  │ (二进制构建器)
               │                └─────────┬─────────┘
               ▼                          │
    ┌───────────────────┐                 │
    │  Dockerfile.base  │ (多运行时基础)   │
    └─────────┬─────────┘                 │
              │                           │
              ▼                           │
    ┌───────────────────┐                 │
    │ Dockerfile.<stack>│ (SDK 依赖层)    │
    └─────────┬─────────┘                 │
              │                           │
              ▼ <────── 延迟注入 (Late) ──┘
    ┌───────────────────┐
    │     最终运行时     │ (启动 <10s)
    └───────────────────┘
```

### 优势 (The Advantage)
- **极速构建**: 修改代码仅会使镜像最后几 MB 失效。即使是 2GB 的 Java 镜像，代码更新后的重构只需 **< 10 秒**。
- **多运行时协同**: 所有镜像均继承自 `hotplex:base`，内置 **Python 3** 与 **Node.js 24**，支持跨语言 Agent 工具（如 MCP Server）。

---

## 📦 可用镜像矩阵

| 镜像 | 标签 | 核心技术栈 | 描述 |
| :--- | :--- | :--- | :--- |
| **基础层** | `hotplex:base` | Debian 12 + Node + Py | 共享 OS 基线，内置全镜像通用工具集。 |
| **Go** | `hotplex:go` | **Go 1.26** | **默认推荐**。集成 `air` 热重载、`dlv` 调试及 `gofumpt`。 |
| **Node** | `hotplex:node` | **Node.js 24** | 集成 `pnpm`, `bun`, `typescript`, `biome`。 |
| **Python** | `hotplex:python` | **Python 3.14** | 集成 `uv`, `poetry`, `ruff`, `pydantic-ai`。 |
| **Rust** | `hotplex:rust` | **Rust 1.94** | 集成 `cargo-nextest`, `cargo-expand`, `cargo-deny`。 |
| **Java** | `hotplex:java` | **JDK 25** | 集成 `gradle`, `maven`, `jbang`, `arthas`。 |
| **全量版** | `hotplex:full` | **All-in-One** | 包含上述所有 SDK，适合复杂跨语言调试场景。 |

---

## 🛠️ 2026 核心工具集 (Tooling Showcase)

### 🧬 通用能力 (Foundation)
- **AI/Agent**: `claude-code`, `opencode-ai`, **MCP Ready**.
- **开发运维**: `gh` (GitHub CLI), `lazygit` (终端 Git UI), `websocat`, `jq`.
- **安全合规**: **`trivy`** (内置安全审计), `envsubst` (环境变量安全插值)。
- **包管理**: `uv` (Python), `bun`, `pnpm`.

### ⚡ 特定栈工具
- **Go**: `air`, `dlv`, `gofumpt`, `golangci-lint`, `sqlc`, `buf`.
- **Python**: `pydantic-ai`, `ruff`, `poetry`, `mypy`.
- **Rust**: `cargo-nextest`, `cargo-expand`, `cargo-deny`, `cargo-watch`.
- **Java**: `jbang`, `arthas`, `async-profiler`.

---

## 🌐 网络与代理配置 (Networking & Proxy)

由于 Docker 的网络隔离，在容器内访问宿主机的代理（如 Clash, V2Ray）需要额外配置：

### 1. 基础配置 (macOS/Windows/Linux)
所有镜像均预配置了 `extra_hosts: ["host.docker.internal:host-gateway"]`。这意味着在容器内可以通过 `host.docker.internal` 访问宿主机。

### 2. 代理注入场景
| 用户场景 | 操作建议 |
| :--- | :--- |
| **标准网络** | 无需任何操作。 |
| **全量代理** | 1. 开启代理软件的 **"允许局域网连接 (Allow LAN)"**。<br>2. 在 `.env` 或 Compose 中注入代理变量：<br> `HTTP_PROXY=http://host.docker.internal:<PORT>` |
| **大模型专用** | 通过 `ANTHROPIC_BASE_URL=http://host.docker.internal:<PORT>` 独立接管 LLM 流量。 |

---

## 🚀 快速上手

管理镜像的最简单方式是通过根目录下的 `Makefile`:

```bash
# 1. 构建默认 Go 环境
make docker-build-go

# 2. 构建特定环境 (例如 python)
make docker-build-stack S=python

# 3. 环境变量切换镜像 (编辑 .env)
HOTPLEX_IMAGE=hotplex:python
```

### 🎛️ 多机器人编排 (Matrix)
对于需要 1+n 机器人协作的场景，请参考 [**HotPlex Matrix 说明文档**](./matrix/README.md)。

---
*由 HotPlex 构建系统生成 - 2026-03*
