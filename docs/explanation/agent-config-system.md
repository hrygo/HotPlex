---
title: Agent 配置系统
weight: 2
description: HotPlex B/C 双通道 Agent 配置：确定性加载、冲突隔离、XML 安全与会话生效边界
---

# Agent 配置系统

> HotPlex 为什么将 Agent 配置分为 B 通道（指令）和 C 通道（上下文），以及这套双通道架构如何通过结构性优先级消除 LLM 的歧义响应。

## 核心问题

HotPlex 是一个多租户、多平台的 AI Agent 接入层。不同用户、不同平台、不同 Bot 可能需要不同的 Agent 行为——从人格语气、工作规则到环境工具使用指南。配置系统需要解决三个核心问题：

1. **优先级冲突**：用户的偏好（"我喜欢 Python"）和工作空间规则（"本项目必须用 TypeScript"）可能矛盾。LLM 无法可靠地判断哪条指令优先。
2. **多租户隔离**：同一个 Slack 工作空间中，不同 Bot 可能有完全不同的人格和规则。一个 Bot 的配置不能泄漏到另一个 Bot。
3. **注入安全**：配置文件由用户编写（Markdown），如果其中包含伪造的 XML 结构标签，可能打破 prompt 的层级结构，实现 prompt injection。

## 设计决策

### B/C 双通道架构

HotPlex 将所有配置分为两个通道，使用 XML 嵌套表达结构性优先级：

```xml
<agent-configuration schema-version="3">
  <directives>
    <hotplex>  META-COGNITION.md (go:embed, 始终首位) </hotplex>
    <persona>  SOUL.md  </persona>
    <rules>    AGENTS.md </rules>
    <tool-guidance> TOOLS.md </tool-guidance>
  </directives>
  <context>
    <notice>   directives 优先声明  </notice>
    <user-data><user> USER.md </user></user-data>
    <memory-data><memory> MEMORY.md </memory></memory-data>
  </context>
</agent-configuration>
```

**B 通道（`<directives>`）**：行为约束，强制性。包含 Agent 的稳定元认知、人格定位、工作规则和环境工具使用指南。`TOOLS.md` 不声明实际工具存在；工具可用性以当前 Session 暴露的结构化工具目录为准。

**C 通道（`<context>`）**：参考信息，辅助性。包含用户偏好、历史交互记录。附带严格的隔离声明："若 [directives] 与 [context] 冲突，以 [directives] 为准。"

**为什么不用单层 system prompt**：

考虑一个场景：用户在 `USER.md` 中写了"我偏好 Python"，但 `AGENTS.md` 中规定"此工作空间必须使用 TypeScript"。如果这些信息在同一层级，LLM 可能根据上下文随机选择。双通道通过三重保障消除歧义：

1. **XML 嵌套结构**：`<directives>` 先出现且被标记为"核心准则"，LLM 解析 XML 时天然赋予更高的注意力权重。
2. **显式冲突声明**：`<notice>` 标签明确告知 Agent 冲突时的优先规则。
3. **位置优先**：B 通道内容始终排在 C 通道之前，利用 LLM 的位置注意力偏差（recency bias 的反向——开头的内容获得更强的"锚定效应"）。

### 命中即终止（Hit-and-Stop）vs 继承（Merge）

每个配置文件通过 3 级 fallback 独立查找：

```
1. dir/{platform}/{botName}/{file} -- Bot 级（最高优先级）
2. dir/{platform}/{file}           -- 平台级
3. dir/{file}                      -- 全局级
```

找到文件就停止，**不会合并多个层级**。`botName` 是 YAML 配置中的 `bots[].name` 字段（如 `"my-bot"`），而非平台运行时 ID。单 Bot 模式下 `botName` 为空，Bot 级查找被跳过，直接从平台级开始解析。

**为什么不用继承（合并所有层级）？**

继承模式会产生"意外的部分覆盖"问题。假设全局 `AGENTS.md` 规定了 10 条规则，平台级 `AGENTS.md` 只写了 2 条。如果合并，最终只有 2 条——其余 8 条被意外丢弃。命中即终止确保每次加载的配置文件是完整的、自洽的。管理员在编写 Bot 级配置时，知道自己写的内容就是最终生效的完整配置，不需要猜测哪些全局规则会被继承。

**文件独立性**：5 个文件（SOUL、AGENTS、TOOLS、USER、MEMORY）各自独立 fallback。文件缺失才进入下一作用域；文件存在但正文为空表示显式清空并立即终止。SOUL.md 可能在 Bot 级命中，而 AGENTS.md 可能 fallback 到全局级。这种设计允许只覆盖需要定制的部分。

AgentConfig 只识别 `SOUL.md`、`AGENTS.md`、`TOOLS.md`、`USER.md`、`MEMORY.md`。真实 Agent Skills 是独立领域，由 `.agents/skills/<name>/SKILL.md` 定义并按需加载，不进入 AgentConfig prompt。

### META-COGNITION：go:embed 的特殊地位

`META-COGNITION.md` 不从文件系统加载，而是通过 Go 的 `//go:embed` 编译进二进制文件：

```go
//go:embed META-COGNITION.md
var embeddedMetacognition string
```

这意味着：

- **始终存在**：即使五个外部配置文件全部缺失或为空，也会生成包含 META 的配置 prompt
- **始终首位**：在 `<directives>` 中排在 `<persona>` 之前
- **不可覆盖**：fallback 机制不适用于此文件，它是嵌入在代码中的

META-COGNITION 定义 Worker 与 Gateway 的身份边界、五文件模型、Tools/Skills 区分、自配置授权事务和生效/验证语义。它不包含动态工具或 Skill catalog、凭据或内部绝对路径，是整个系统稳定的认知与安全基线。

### Session Runtime Facts：schema v3 的边界

实时 Worker 的 system prompt 使用外层 `<agent-configuration schema-version="3">`。有运行时事实时，Gateway 会在 `<directives>` 之前插入一个受限的
`<runtime-facts format="application/json" schema-version="1">` 块；这里的 `schema-version="1"` 是 facts 载荷版本，不是外层 prompt schema。Admin 预览继续调用不带 facts 的 `BuildSystemPrompt`，因此不会伪造某个运行中的 Session。

Runtime facts 在 Worker 已选定、Session 信息已解析后由 Gateway 构建；首次启动和 `/reset` 共用同一个注入边界。它只声明当前 Session 的有限事实：平台、Worker 类型、作用域种类、声明的权限模式、`resume`/`streaming`/`tools` 能力、`skills`/`mcp`/`worker` 查询面、Skill catalog 所有者，以及 allowlist Gateway 环境键名的存在性。

这些是**声明（declared）而不是观测或强制执行证明（observed/enforced）**：它们不能证明外部 Worker 健康、权限实际生效或某个命令已经成功。载荷不包含身份值、Session/频道/线程/团队 ID、工作目录、环境变量值、凭据、动态 catalog、Skill 的 `name`/`description`/正文或 MCP 配置。Agent 不得从缺失的事实或文档推断出未暴露的能力。

### Agent Skills 的发现、所有权与调用

Admin API 与 WebChat HTTP Skills read surface 指向真实 Agent Skills，不是 AgentConfig 的 `TOOLS.md`
槽位。Session `/skills` 仍由当前 Worker/filesystem evidence 决定；原生 Worker 负责向模型做
`name`、`description` 的 progressive disclosure 和按需加载。HotPlex 不把动态 Skill catalog
重复注入 AgentConfig prompt。

Skill 状态按当前 Session 的证据区分：文件系统找到定义但没有当前 Worker 调用证据是 `discoverable`；Worker 权威目录确认可原生执行才是 `callable`；`unavailable` 保留给能力表面明确报告的不可用状态。只有 `callable` 可以调用。短 `/name`（包括 WebChat）、显式 `/worker <name>`、busy replay 和 crash structured replay 都复用同一个 Session callability 判定；filesystem-only Skill 永远不能绕过它变成可调用。

内置 Skill 是 Admin/WebChat HTTP read surface 的真实 Agent Skills 只读发现项：`hotplex-cli` 面向日常
Cron、显式 Slack 和只读诊断，`hotplex-operator` 面向服务、更新、配置、Admin 与审计，后者需要
显式 operator authority。它们不要求 projection、inventory 或 receipt 存在即可被这些 HTTP 列表发现；
Session `/skills` 的出现由当前 Worker/filesystem evidence 决定；filesystem-only 项是
`discoverable`，只有 native advertisement 和 adapter-verified activation 才能证明 `callable`。
真实 global/project/user Skill 优先遮蔽同名内置项，`source` 仍只表示 `global` 或
`project`，builtin 元数据只通过可选 `builtin`/`builtin_package_version` 字段表达。没有同名真实项
时，内置项 update/delete 返回 `SKILL_BUILTIN_READONLY`；创建同名用户 override 仍走正常 Skill CRUD。

两个内置包的 canonical 来源是 `internal/skills/builtin/hotplex-cli` 与
`internal/skills/builtin/hotplex-operator`。生成器产出 byte-identical 的
`.agents/skills/hotplex-cli` 和 `.agents/skills/hotplex-operator` mirror。仓库 portfolio 恰好包含
`hotplex-cli`、`hotplex-operator`、`hotplex-diagnostics`、`hotplex-release`、
`hotplex-docs-patrol` 五个 Skill。

原生 Skill 同步把 UserHome 与 `$HOTPLEX_HOME` 分开：Claude 使用 `<UserHome>/.claude/skills`，Codex/OpenCode 共享 `<UserHome>/.agents/skills`，ACP 没有可推断的 filesystem root；immutable inventory、状态和 receipts 位于 `$HOTPLEX_HOME`。`hotplex skills status|sync|remove` 使用 runtime（`hotplex-cli`）或 operator（累积包含 `hotplex-cli` 与 `hotplex-operator`）profile，可重复传入 `--worker`，并支持 `--dry-run`/`--json`。未显式指定 worker 时只采用已启用 messaging platform/bot 的解析结果；空目标返回 bounded error，不回退到注册表。remove 只删除 receipt 与 unchanged-tree 能证明归属的 native projection，不删除 inventory。

Gateway startup 的 built-in reconciliation check 与 `doctor` 的 built-in Skills checker 只读。
`onboard`/`update` 只有显式 `--sync-skills`
才同步（`update` 可用 `--skills-profile` 选择累积 profile）。同步遇到 collision、drift 或 failed
item 以非零结果结束，也不会覆盖未知 user/project Skill；新 Session 或 `/reset` 后才重新看到新的
Worker 目录证据。

### CLI 与 Cron 路由

处理 Cron 请求时，优先路由到当前 Session 实际暴露的 `hotplex-cli` Skill；Skill 不可用时，查询当前安装二进制而不是依赖旧示例：`hotplex cron --help`，再按需查看 `hotplex cron create --help`。创建后必须使用独立读取路径执行 `hotplex cron get <id|name> --json`，核对任务状态、schedule、platform 和 platform key；无法调用 CLI 时应明确返回 `unsupported`/degraded。

### 自配置事务与能力边界

当用户要求 Agent 调整自身时，META 要求遵循固定事务：

```
inspect → explain → propose diff → request approval → validate → atomic apply → activate → verify
```

- 先检查当前作用域、有效来源和版本，再解释为什么需要修改；
- 只修改用户授权的当前 Bot/Workspace 槽位，不通过 global 配置间接影响其他租户；
- 写入前展示 diff 并获得批准，写入时执行白名单、大小、并发版本和原子性校验；
- 激活边界是新 Session 或明确 `/reset`，随后验证有效来源和行为；
- 当前 Worker/宿主若没有暴露受控写接口，只能给出提案，必须明确报告 `unsupported`，不能直接编辑未知路径或声称配置已经生效。

因此，META 提供的是稳定的决策协议，而实际读写权限仍由 Gateway 或宿主在当前 Session **实际暴露**的类型化控制面决定。提示词本身不构成授权。

## 内部机制

### 配置加载流程

`Load(dir, platform, botName, injectExclude...)` 的完整执行路径：

```
1. 路径安全检查：ValidateBotName(botName)（防止路径穿越）
2. 逐文件加载（SOUL → AGENTS → TOOLS → USER → MEMORY）：
   a. 检查 injectExclude：如文件名在排除列表中，跳过加载
   b. 调用 resolveFile(dir, platform, botName, fileName)
   c. 按三级 fallback 查找；缺失继续，present-empty 命中并显式清空
   d. 读取文件内容，剥离 YAML frontmatter
   e. 检查单文件大小限制（MaxFileChars = 8000 字符）
   f. 检查总量预算（MaxTotalChars = 40000 字符）
   g. 超出预算的文件截断并记录 warning
3. 返回 AgentConfigs 结构体
```

### YAML Frontmatter 剥离

配置文件支持 Hugo 风格的 YAML frontmatter（`---` 包裹的元数据块），Gateway 在加载时自动剥离。Frontmatter 是给配置管理系统（如 Git、CMS）使用的元数据，不是给 LLM 看的。剥离操作节省 Worker 的 token 消耗。

剥离逻辑处理畸形 frontmatter 的策略是"原样返回"——如果找不到闭合的 `---`，说明格式错误，不会截断内容，而是保留原文。

### 文件排除（inject_exclude）

`inject_exclude` 允许管理员跳过指定配置文件的加载，被排除文件对应的注入位置保持为空。这在多 Bot 场景下特别有用——例如某些 Bot 不需要 `MEMORY.md` 或 `USER.md` 的上下文。

**三级 fallback**（通过 `ResolveInjectExclude` 解析）：

```
1. Bot 级（bots[].inject_exclude）           -- 最高优先级
2. 平台级（messaging.*.inject_exclude）
3. 全局级（agent_config.inject_exclude）      -- 默认
```

解析规则：
- **非 nil 空切片**（YAML `inject_exclude: []`）表示"显式清空"——即使全局级有值，也会被覆盖为空
- **nil**（未设置）表示"使用上级值"——fallback 到上级配置
- **非空切片** 表示"使用此列表"——直接覆盖上级配置

**匹配方式**：大小写不敏感。`SOUL.md` 和 `soul.md` 等效；只匹配五个规范槽位。

**不可排除**：`META-COGNITION.md` 通过 `go:embed` 编译进二进制，始终注入，不受 `inject_exclude` 影响。

**配置示例**：

```yaml
# 全局：排除所有 Bot 的 MEMORY.md
agent_config:
  inject_exclude: ["MEMORY.md"]

# 平台级：Slack 平台排除 SOUL.md 和 MEMORY.md
messaging:
  slack:
    inject_exclude: ["SOUL.md", "MEMORY.md"]

# Bot 级：特定 Bot 排除 USER.md 和 MEMORY.md
messaging:
  slack:
    bots:
      - name: "dev-bot"
        inject_exclude: ["USER.md", "MEMORY.md"]
```

### XML Sanitizer：防止注入

`sanitize()` 函数对配置内容中的保留 XML 标签进行 HTML 转义：

```go
var reservedTags = []string{
    "agent-configuration", "directives", "context", "persona",
    "rules", "skills", "tool-guidance", "runtime-facts",
    "user", "memory", "user-data", "memory-data", "hotplex", "notice",
}
```

如果 `SOUL.md` 中包含 `<directives>` 字面量（例如在 Markdown 代码块中解释系统结构），它会被转义为 `&lt;directives&gt;`，防止用户通过配置文件注入伪造的指令层级。

转义同时覆盖大小写变体（`<DIRECTIVES>` 和 `<directives>`），防止通过大小写绕过。

### Prompt 组装：BuildSystemPrompt

`BuildSystemPrompt(configs)` 将加载的配置组装成最终的 system prompt：

```
1. 构建 B 通道：
   - hotplex 元认知（go:embed，始终存在）
   - <persona> 包裹 SOUL.md
   - <rules> 包裹 AGENTS.md
   - <tool-guidance> 包裹 TOOLS.md，并声明它不是工具可用性目录
   - 外层用 <directives> 包裹并附加优先级声明

2. 构建 C 通道：
   - <notice> 插入冲突隔离声明
   - <user-data>/<user> 包裹 USER.md，明确标记为数据
   - <memory-data>/<memory> 包裹 MEMORY.md，明确标记为数据
   - 外层用 <context> 包裹

3. 外层用 <agent-configuration schema-version="3"> 包裹全部；实时 Session 在 directives 前可选插入 schema 1 的 runtime facts
```

### Worker 注入差异

不同 Worker 类型接收配置的方式不同：

| Worker | 注入方式 | 原因 |
|--------|---------|------|
| Claude Code | `--append-system-prompt` | CC 原生支持追加 system prompt |
| OpenCode Server | `system` 字段 | OCS 使用 HTTP API，system 是消息字段 |

Windows 上 Claude Code 还额外使用 `--append-system-prompt-file`（临时文件注入），避免 cmd.exe 截断长参数。

### 大小限制的来源

```go
const MaxFileChars = 8_000    // 单文件上限
const MaxTotalChars = 40_000  // 总量上限
```

Claude Code 的 context window 中，system prompt 占用的 token 直接减少了可用于对话的空间。一个 40KB 的 system prompt 大约消耗 10K-15K token，对于一个 200K context window 的模型来说是可接受的。超过这个预算会显著影响 Agent 的对话质量。

加载时按文件逐一累加总量，超出预算的文件会被截断并记录警告日志。

## 权衡与限制

1. **配置修改不即时生效**：配置只在 Session 初始化或 `/reset` 时加载。这意味着修改 SOUL.md 后，正在运行的对话不会立即反映变化。这是有意为之——防止 mid-conversation personality shift（对话中途人格切换）导致用户体验混乱。

2. **截断静默化**：当文件超过预算被截断时，只记录 warning 日志，不返回错误。调用方（Bridge）无法感知配置被截断。在极端情况下，Agent 可能因为截断丢失关键规则。

3. **无配置校验**：系统不校验配置内容的语法或语义。格式错误的 Markdown（如未闭合的代码块）会原样传递给 Worker，可能导致 LLM 解析困惑。

4. **fallback 不可跨文件共享**：每个文件独立 fallback。如果 Bot 级 `SOUL.md` 引用了 `AGENTS.md` 中定义的术语，但 `AGENTS.md` 是全局级的，两个文件的上下文可能不一致。管理员需要确保不同层级的配置在语义上兼容。

## 参考

- `internal/agentconfig/loader.go` -- 3 级 fallback 加载逻辑
- `internal/agentconfig/prompt.go` -- B/C 通道组装与 XML Sanitizer
- `internal/agentconfig/META-COGNITION.md` -- go:embed 元认知层

---

## 相关实践

- [Agent 人格定制教程](../tutorials/agent-personality.md) — 手把手创建 SOUL.md / AGENTS.md 定制 Agent 行为
