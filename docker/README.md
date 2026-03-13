# HotPlex Docker 生态系统

## 🚀 架构图

```text
    [ 官方 SDK 镜像 ]           [ HotPlex 源码 ]
     (Node, Python, Java)                │
               │                         ▼
               │                ┌───────────────────┐
               │                │ Dockerfile.artif  │ (二进制构建器)
               │                └─────────┬─────────┘
               ▼                          │
    ┌───────────────────┐                 │
    │  Dockerfile.base  │ (静态 OS 层)    │
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

## 🚀 延迟注入架构 (Late-Injection)

为了缩短“代码到容器”的反馈循环，我们将稳定的语言环境（SDK）与易变的应用程序二进制文件 (`hotplexd`) 解耦。

1.  **静态基础层 (`hotplex:base`)**: 通用基础（Debian Bookworm），包含系统工具和环境设置。一次构建，长期缓存。
2.  **二进制提供者 (`hotplex:artifacts`)**: 极速 Go 构建器，仅编译 `hotplexd` 二进制文件。
3.  **技术栈变体 (`hotplex:go`, `:node` 等)**: 继承自 `base` 并安装特定 SDK。
4.  **延迟注入**: `hotplexd` 二进制文件从 `artifacts` 提供者复制而来，作为每个变体的**最后一个层**。

**优势**: 修改 Go 代码只会使镜像的最后几 MB 失效。在代码更改后，重新构建 2GB 的 Java 镜像只需 **< 10 秒**。

---

## 📦 可用镜像

| 镜像           | 标签                | 描述                                                       |
| :------------- | :------------------ | :--------------------------------------------------------- |
| **基础层**     | `hotplex:base`      | 共享 OS 基线 + `websocat` + NPM 全局工具。                 |
| **产出物**     | `hotplex:artifacts` | `hotplexd` 二进制文件的内部提供者。                        |
| **Go**         | `hotplex:go`        | **默认。** Go 1.26 SDK + `air` + `dlv` + `golangci-lint`。 |
| **Node**       | `hotplex:node`      | Node.js 24 + `pnpm` + `bun` + `typescript`。               |
| **Python**     | `hotplex:python`    | Python 3.14 + `uv` + `poetry` + `ruff`。                   |
| **Java**       | `hotplex:java`      | Temurin 25 JDK + `gradle` + `maven`。                      |
| **Rust**       | `hotplex:rust`      | Rust 1.94 + `cargo-watch` + `nextest`。                    |
| **全量版**     | `hotplex:full`      | 包含上述所有 SDK 的全功能技术栈。                          |

---

## 🛠️ 构建与执行

管理这些镜像最简单的方法是通过根目录下的 `Makefile`:

### 基础指令
```bash
# 构建默认的 Go 技术栈
make docker-build-go

# 构建特定技术栈 (例如 node)
make docker-build-stack S=node

# 构建所有技术栈
make docker-build-all
```

### 环境配置
更新 `.env` 文件来选择激活的镜像：
```bash
HOTPLEX_IMAGE=hotplex:go
```

## 🎛️ 容器编排：HotPlex Matrix

HotPlex Matrix 是默认的多机器人编排方案，支持 1+n 机器人协作模式。该架构通过以下设计确保环境的稳健性：

- **组合式继承 (`extends`)**: 利用 Docker Compose 的原生继承机制合并公共配置（网络、资源限制）与实例特有配置（端口、标识）。
- **物理隔离**: 每一个机器人实例拥有独立的宿主机挂载路径 `~/.hotplex/instances/${HOTPLEX_BOT_ID}/`，确保数据与状态的物理隔离。
- **自动化预备**: 通过 `make docker-prepare` 自动扫描环境配置并初始化所有机器人的宿主机目录树，实现“约定大于配置”的部署流程。

详细的架构描述与工作流，请参阅 [Matrix 说明文档](./matrix/README.md)。

---

## 🛠️ 运行时环境

所有运行镜像均集成了以下核心组件与机制：

### 1. 核心工具集
- `websocat`: WebSocket 调试工具。
- `claude-code` & `opencode-ai`: 智能编码助理与 Agent 工具。
- `jq`, `yq`, `curl`, `git`: 基础运维工具。

### 2. 安全变量插值 (`envsubst`)
容器启动时会自动处理配置文件中的环境变量。为防止破坏系统提示词（System Prompt）中的代码示例（如 `${issue_id}`），系统通过白名单机制仅处理 `HOTPLEX_`, `GIT_`, `GITHUB_`, `HOST_` 前缀的变量。

---
*由 HotPlex 构建系统生成 - 2026*
