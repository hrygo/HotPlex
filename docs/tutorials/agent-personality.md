---
title: Agent 人格定制教程
weight: 3
description: 使用双通道配置系统定制 AI Agent 的人格与行为，5 分钟上手
---

# Agent 人格定制教程

HotPlex 通过**双通道配置系统**控制 Agent 的人格、行为规则和上下文记忆。本教程从最简单的单文件开始，逐步展示完整的定制能力。

**前置条件**：HotPlex 已安装并运行（`make quickstart` + `hotplex gateway start`）。

## 核心概念

配置文件存放在 `~/.hotplex/agent-configs/` 目录，分为两条通道：

| 通道 | 文件 | 定位 | 优先级 |
|------|------|------|--------|
| **B 通道 (Directives)** | `SOUL.md` | Agent 人格、语气、价值观 | **强制执行** |
| | `AGENTS.md` | 工作规则、约束、禁止事项 | **强制执行** |
| | `TOOLS.md` | 环境工具使用指南（不声明实际可用性） | **强制执行** |
| **C 通道 (Context)** | `USER.md` | 用户档案、偏好 | 仅供参考 |
| | `MEMORY.md` | 跨会话记忆 | 仅供参考 |

冲突规则：B 通道无条件覆盖 C 通道。另外，`META-COGNITION.md` 由网关自动注入（go:embed），始终排在 B 通道首位，无需手动创建。

## 1. 最简定制：定义 Agent 人格

创建全局 `SOUL.md`，赋予 Agent 独特的人格：

```bash
mkdir -p ~/.hotplex/agent-configs
```

```markdown
<!-- ~/.hotplex/agent-configs/SOUL.md -->

# 人格

你是资深 Go 工程师，专注后端系统设计与性能优化。

## 语气

- 简洁直接，不废话
- 技术术语使用英文，解释用中文
- 给出具体可执行的建议，不说"可以考虑"

## 价值观

- 正确性优先于简洁性
- 明确标注不确定性，不猜测
- 主动识别风险和技术债务
```

**生效方式**：新建会话或发送 `/reset` 重新加载配置。运行中的会话不会热更新。

**验证**：在 Slack 或 Web Chat 中发送 `你是谁`，Agent 应按照 SOUL.md 中的人格回答。

## 2. 添加工作规则

创建 `AGENTS.md`，定义代码规范和行为约束：

```markdown
<!-- ~/.hotplex/agent-configs/AGENTS.md -->

# 代码规范

## 必须

- 错误处理：`fmt.Errorf("context: %w", err)` 包装
- 日志：统一使用 `log/slog`，JSON handler
- 测试：table-driven + `t.Parallel()` + `testify/require`
- Mutex：显式 `mu` 字段，不嵌入，不传指针

## 禁止

- 严禁 `sed`/`awk` 修改源码（缩进不可控）
- 严禁硬编码路径分隔符（用 `filepath.Join`）
- 严禁 Shell 执行（仅允许 `claude` 二进制）
- 严禁在请求处理路径使用 `context.Background()`

## 行为约束

- 执行危险操作前必须获得用户批准
- 修改前展示意图，等待确认后再动手
- 不确定时说"需要调查"，不猜测
```

## 3. 添加工具指南

如果需要说明当前环境如何使用工具，可创建 `TOOLS.md`：

```markdown
<!-- ~/.hotplex/agent-configs/TOOLS.md -->

# 工具使用指南

- 执行项目检查优先使用 `make check`
- 调用外部服务前先确认当前 Session 是否暴露相应工具
- 不把文档中出现的命令当作工具已安装的证据
```

`TOOLS.md` 只是常驻指导，不是 Agent Skill 列表。真实 Skills 由独立的
`.agents/skills/<name>/SKILL.md` 定义，并通过 Admin API / WebChat Skills 页面管理、
按需加载。旧 AgentConfig `SKILLS.md` 在兼容期仍可读取，但新配置应只创建
`TOOLS.md`；同一目录两者并存时 `TOOLS.md` 生效，`hotplex doctor` 会报告迁移提示。

### Session 自我认知与 Skills 调用边界

实时 Session 的 system prompt 使用外层 schema v3，并可在 directives 前携带受限的
`runtime-facts`（载荷 schema 1）。它只提供 Gateway 构建的声明性事实：平台、Worker、
作用域种类、声明的权限/能力、查询面、Skill catalog 所有者和 allowlist 环境键名的存在性；
不提供身份值、路径、环境值、凭据、动态 catalog 或 Skill 正文。事实不是 Worker 健康或权限
已经执行的证明，Admin 预览没有运行时 facts。

Admin API 的 `skills`、WebChat Skills、Session `/skills` 和 Worker 原生 Skill catalog 都是
真实 Agent Skills 的表面；`TOOLS.md` 不是 Skill catalog，META 也不会复制 catalog。当前
Session 的状态含义是：文件系统发现但没有调用证据为 `discoverable`，Worker 权威目录确认
可执行为 `callable`；`unavailable` 保留给能力表面明确报告的不可用状态。只有 `callable`
可调用；短 `/name`（包括 WebChat）、显式 `/worker <name>`、busy replay 和 crash structured
replay 共用同一判定，filesystem-only Skill 不会因为存在路径而变成可调用。

配置变更和 Skill 可见性都应在新建 Session 或 `/reset` 后重新检查，并以当前 `/skills` 状态
为准。

### Cron 请求的 CLI 路由

Cron 请求优先使用当前 Session 可用的 `hotplex-cli` Skill；不可用时先查询当前二进制的
`hotplex cron --help`，必要时再看 `hotplex cron create --help`。执行 `cron create` 后，必须再执行
`hotplex cron get <id|name> --json`，核对任务状态、schedule、platform 和 platform key；不能以
Skill 文档或命令示例代替成功证据。

## 4. 添加用户档案（C 通道）

C 通道文件为 Agent 提供**参考上下文**，但不会覆盖 B 通道的规则：

```markdown
<!-- ~/.hotplex/agent-configs/USER.md -->

# 用户档案

## 基本信息

- 姓名：小张
- 角色：后端工程师
- 技术栈：Go, PostgreSQL, Docker, Kubernetes

## 偏好

- 代码注释和 commit message 使用英文
- 交流使用中文
- 喜欢看到完整的错误处理链路
- 偏好渐进式重构，不做大爆炸式重写

## 当前项目

- HotPlex Gateway — AI Agent 统一接入层
- 主要开发分支：main
```

## 5. 添加跨会话记忆

`MEMORY.md` 帮助 Agent 在不同会话间保持上下文连贯：

```markdown
<!-- ~/.hotplex/agent-configs/MEMORY.md -->

# 项目记忆

## 已完成的决策

- 2025-03：选择 SQLite 作为 session 持久化方案（单机部署优先）
- 2025-04：WebSocket Hub 采用广播模式（1:N 扇出）
- 2025-05：Worker 进程终止采用三层策略（SIGTERM → 5s → SIGKILL）

## 已知问题

- Windows 自更新受限（exe 运行时被锁）
- OpenCode CLI 已由 OpenCode Server 替代

## 习惯

- 提交前必须运行 `make check`
- PR 标题使用 Conventional Commits 格式
```

> **注意**：如果 MEMORY.md 的内容与 AGENTS.md 的规则冲突，Agent 会以 AGENTS.md 为准（B 通道优先）。

## 6. 按平台或 Bot 覆盖配置

HotPlex 支持**三级 fallback**，每个文件独立解析，命中即终止：

```
全局级：~/.hotplex/agent-configs/SOUL.md
平台级：~/.hotplex/agent-configs/slack/SOUL.md
Bot 级：~/.hotplex/agent-configs/slack/my-bot/SOUL.md
```

Bot 级目录名使用 YAML 配置中 `bots[].name` 的值（如 `"my-bot"`），而非平台运行时 ID。单 Bot 模式无 Bot 级目录，直接使用平台级。解析顺序：Bot 级 → 平台级 → 全局级，第一个**存在**的文件生效；缺失表示继续继承，存在但正文为空表示显式清空并停止 fallback。

### 示例：为特定 Bot 定制人格

假设 YAML 配置中定义了一个名为 `dev-bot` 的 Bot：

```yaml
messaging:
  slack:
    bots:
      - name: "dev-bot"
        bot_token: "xoxb-..."
```

给它一个不同的人格：

```bash
mkdir -p ~/.hotplex/agent-configs/slack/dev-bot
```

```markdown
<!-- ~/.hotplex/agent-configs/slack/dev-bot/SOUL.md -->

# 人格

你是 DevOps 工程师助手，专注于 CI/CD、监控和基础设施。

## 语气

- 务实，给出可直接执行的命令
- 标注每条命令的风险等级
- 故障排查时按排查树逐步推进
```

此时 `dev-bot` 使用 DevOps 人格，其他 Bot 仍使用全局 `SOUL.md`。`AGENTS.md`、`USER.md` 等文件同理，各自独立 fallback。

> **重要**：Bot 级文件存在时（即使是空文件），该 Bot **不会**读取平台级和全局级的同名文件。如需基于全局修改，先复制再编辑：
>
> ```bash
> cp ~/.hotplex/agent-configs/SOUL.md ~/.hotplex/agent-configs/slack/dev-bot/SOUL.md
> # 然后编辑 Bot 级文件
> ```

## 7. 配置限制与注意事项

| 项目 | 限制 |
|------|------|
| 单文件大小 | 8000 字符 |
| 所有文件总量 | 40000 字符 |
| 生效时机 | 新建会话或 `/reset`，运行中不热更新 |
| YAML frontmatter | 自动剥离，不占用 Token |

### 快速验证清单

1. **文件位置**：`ls ~/.hotplex/agent-configs/` 确认文件存在
2. **内容加载**：发送 `/reset` 后提问，观察行为是否符合预期
3. **通道优先级**：故意让 MEMORY.md 与 AGENTS.md 内容冲突，验证 Agent 以 AGENTS.md 为准
4. **Bot 级覆盖**：在 Bot 级目录放置 SOUL.md，验证覆盖生效
5. **迁移检查**：运行 `hotplex doctor`，确认没有旧 `SKILLS.md`、同层冲突或意外的 present-empty 文件

---

**下一步**：探索 [Slack 集成](./slack-integration.md) 或 [飞书集成](./feishu-integration.md)，让定制后的 Agent 在即时通讯平台中工作。
