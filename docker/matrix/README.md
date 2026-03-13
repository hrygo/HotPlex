# 🤖 HotPlex Matrix: 1+n 机器人编排

HotPlex Matrix 是一个支持 1+n 机器人协作的编排架构，允许一个主机器人 (hotplex-01) 与多个从机器人 (hotplex-nn) 隔离运行。该架构实现了**自动化部署 (Automation)**、**实例隔离 (Isolation)** 与 **配置便携性 (Portability)**。

---

## 🏗️ 技术架构

### 1. 组合式继承模型
为了杜绝配置冗余并保证列表合并的正确性，我们采用了 Docker Compose 原生的 `extends` 机制：
- **`hotplex-base`**: 核心抽象层，定义了所有机器人的公共配置（环境变量白名单、健康检查、日志策略、计算资源限制）以及共享卷（配置种子、构建缓存）。
- **具体机器人服务**: 通过 `extends` 继承基础层，并在此基础上叠加特有的容器名称、端口映射及**物理隔离路径**。

### 2. 物理隔离协议
每个机器人拥有独立的宿主机工作目录，防止数据交叉污染：
- **存储隔离**: `~/.hotplex/instances/${HOTPLEX_BOT_ID}/storage` -> `/home/hotplex/.hotplex`
- **工作区隔离**: `~/.hotplex/instances/${HOTPLEX_BOT_ID}/projects` -> `/home/hotplex/projects`
- **配置隔离**: 每个机器人通过各自的 `.env.xxx` 文件注入不同的 `HOTPLEX_BOT_ID` 与各平台凭据。

### 3. 多层安全性设计
- **`envsubst` 白名单**: 容器入口点 (`docker-entrypoint.sh`) 仅会对 `HOTPLEX_`, `GIT_`, `GITHUB_`, `HOST_` 前缀的环境变量进行插值，从而**完美保留**系统提示词中的 Shell 示例（如 `${issue_id}`）。
- **权限自动纠偏**: 采用“约定大于配置”方案，所有挂载目录在容器启动前由 `Makefile` 以宿主机用户权限预先创建，避免 Docker 自动创建 `root` 权限目录。

---

## 🚀 启动工作流程

系统通过 `Makefile` 实现三阶段启动流水线：

```text
[ 执行 make docker-up ]
        │
        ▼
1. 环境准备 (docker-prepare)  ──► [ 扫描 .env.* 提取 Bot ID ]
        │                    ──► [ 创建 ~/.hotplex/instances/ID/{storage,claude,projects} ]
        ▼
2. 配置同步 (docker-sync)     ──► [ 同步项目 configs/ 到宿主机运行目录 ]
        │
        ▼
3. 容器启动 (docker compose)  ──► [ 加载从服务继承基础模板 (extends) ]
                             ──► [ 顺序启动 hotplex-01 及 n 个 hotplex-nn 容器 ]
        │
        ▼
4. 容器内初始化 (Boot)        ──► [ 环境变量插值 (仅限白名单变量) ]
                             ──► [ 关键配置注入 (配置种子、技能、团队) ]
                             ──► [ 最终拉起 HotPlex Engine ]
```

### 关键指令
| 指令 | 作用 |
| :--- | :--- |
| `make docker-prepare` | **预备阶段**: 动态发现所有机器人实例并初始化宿主机目录树。 |
| `./add-bot.sh` | **新增机器人**: 交互式向导，快速添加并配置新的机器人实例。 |
| `make docker-sync` | **同步阶段**: 将宿主机 `configs/` 目录下的 YAML 规则同步至运行目录。 |
| `make docker-up` | **启动全流程**: 自动执行准备与同步过程，随后拉起所有机器人容器。 |
| `make docker-down` | **关停环境**: 停止并移除所有容器。 |

---

## 📂 端口与目录映射矩阵

| 机器人角色 | 外部访问 (Host) | 容器内部 (Container) | 物理路径说明 |
| :--- | :--- | :--- | :--- |
| **hotplex-01** | `127.0.0.1:18080` | `8080` | `~/.hotplex/instances/U0AHRCL1KCM/` |
| **hotplex-02** | `127.0.0.1:18081` | `8080` | `~/.hotplex/instances/U0AJVRH4YF6/` |
| **共享资源** | 不适用 | `/home/hotplex/configs` | `~/.hotplex/configs/` (全局配置) |

---

## 📄 配置文件说明

- **`docker-compose.yml`**: 生产环境运行定义，默认使用远程预构建镜像。
- **`docker-compose.build.yml`**: 本地开发/构建定义，用于源码编译与调试。
- **`.env-01`**: 主机器人的环境变量，控制主节点的身份与行为。
- **`.env-02`**: 从机器人的环境变量，用于横向扩展节点。

---

> [!IMPORTANT]
> **配置习惯**：
> - **行为规则**：Slack/Feishu 的 AI 身份、工作流逻辑等非敏感规则存储在 `configs/` 中，修改后运行 `make docker-sync` 即可同步。
> - **敏感信息**：各平台 Token、密钥等敏感凭据均通过项目根目录的 `.env` 文件进行管理，实现规则与凭据的物理分离。
