# 🤖 HotPlex Matrix: 1+n 多机器人编排

HotPlex Matrix 是默认的容器编排方案，支持一个主机器人 (hotplex-01) 与多个从机器人 (hotplex-nn) 的隔离运行与协同。

---

## 🏗️ 核心设计原则

### 1. 组合式继承 (Extends)
我们利用 Docker Compose 的 `extends` 机制实现了“原子化”配置：
- **`hotplex-base`**: 定义公共网络、计算资源限制、环境变量白名单及共享卷。
- **实例定义**: 每个机器人服务仅声明其特有的端口映射与**物理隔离路径**。

### 2. 物理隔离 (Isolation)
为了确保数据安全与状态一致性，每个机器人实例拥有完全独立的宿主机目录：
- **路径格式**: `~/.hotplex/instances/${HOTPLEX_BOT_ID}/`
- **隔离内容**: 包含 `storage` (数据库/会话)、`projects` (代码仓库) 与 `claude` (Agent 配置)。

---

## 🚀 自动化启动流水线

通过 `make docker-up` 一键触发以下流程：

```text
1. 准备阶段 (Prepare) ──► 扫描 .env.* 自动创建宿主机隔离目录树
2. 同步阶段 (Sync)    ──► 将全局 configs/ 规则下发至各机器人实例
3. 编排阶段 (Compose) ──► 加载继承模板，拉起多容器集群
4. 初始化阶段 (Boot)   ──► 环境变量插值、Git 身份注入、拉起核心引擎
```

### 运维指令表
| 指令 | 描述 |
| :--- | :--- |
| `make docker-up` | **全流程启动**: 自动完成准备、同步并拉起所有容器。 |
| `./add-bot.sh` | **横向扩展**: 快速引导添加第 n 个机器人实例。 |
| `make docker-sync` | **规则热更新**: 仅同步 `configs/` 变更（无需重启容器）。 |
| `make docker-down` | **关停环境**: 停止并清理集群资源。 |

---

## 📂 部署矩阵示例

| 实例名称 | 外部端口 | 隔离路径 (宿主机) | 镜像版本 (示例) |
| :--- | :--- | :--- | :--- |
| **hotplex-01** | `18080` | `~/.hotplex/instances/ID_A/` | `hotplex:go` |
| **hotplex-02** | `18081` | `~/.hotplex/instances/ID_B/` | `hotplex:python` |

---

## 📄 配置文件管理

- **`.env` (根目录)**: 存储敏感信息（Token, API Keys），实现全局凭据管理。
- **`.env-01`, `.env-02`**: 定义各机器人的身份 ID (Bot ID) 与特有行为。
- **`configs/`**: 存储非敏感的业务规则与智控逻辑，由所有实例共享。

---

### 💡 提示
- 容器入口点会自动处理环境变量插值，但**仅限** `HOTPLEX_`, `GIT_`, `GITHUB_` 等前缀变量，以保护 Prompt 中的原生脚本。
- 更多镜像技术细节，请参阅 [**HotPlex Docker 生态概览**](../README.md)。

---
*由 HotPlex 构建系统生成 - 2026-03*
