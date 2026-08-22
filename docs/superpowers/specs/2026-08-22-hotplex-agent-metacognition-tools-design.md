# HotPlex 元认知、TOOLS 与自配置能力设计

**状态**：已批准  
**日期**：2026-08-22  
**范围**：AgentConfig、系统提示词装配、真实 Agent Skills 边界、安全自配置控制面、Worker 系统提示词交付能力

## 1. 背景

`internal/agentconfig/META-COGNITION.md` 原本同时描述 HotPlex 架构、自身配置模型和操作约束。安全加固为避免把私有 AgentConfig、Skill 正文和内部路径降级拼入普通用户消息，大幅压缩了该文件。安全目标正确，但当前版本只保留运行时禁止事项，无法支撑 Agent 对 HotPlex 的清晰自我认知和安全自配置。

当前 AgentConfig 还使用 `SKILLS.md` 表示工具使用指南，并在提示词装配阶段将其错误建模为 Skill catalog。与此同时，HotPlex 已有独立的真实 Agent Skills 系统，通过 `.agents/skills/<name>/SKILL.md` 等目录完成发现、管理和按需调用。两者名称重叠，已造成代码、API、UI 和文档语义混淆。

本设计不恢复旧版 META 的动态实现细节，也不把真实 Skills 合并进 AgentConfig。它建立稳定元认知、五文件 AgentConfig、运行时工具目录和真实 Skills 四个相互独立的领域。

## 2. 目标

1. 让 Worker 清晰理解自身在 HotPlex 中的身份、责任边界、配置模型和激活生命周期。
2. 让 Worker 能通过受控、可审计、需用户批准的类型化接口调整当前 AgentConfig。
3. 将 AgentConfig 的工具指南槽位从 `SKILLS.md` 规范化为 `TOOLS.md`。
4. 保持真实 Agent Skills 的 `SKILL.md`、Admin API、WebChat 管理页、Worker 命令和渐进式加载语义不变。
5. 确保 META 始终通过真正的系统提示词通道交付；不支持的 Worker 必须显式报告能力缺失。
6. 保留旧 `SKILLS.md` 的有限迁移兼容，不静默覆盖或删除用户配置。

## 3. 非目标

- 不让 Agent 任意编辑 HotPlex 主配置、凭据、模型路由、认证配置或服务拓扑。
- 不把 `TOOLS.md` 作为权威工具发现协议。
- 不把 Skill 正文常驻注入系统提示词。
- 不通过普通用户消息模拟系统提示词。
- 不在 META 中固化内部绝对路径、具体超时、Issue 编号、Worker 实现参数或动态工具清单。
- 不修改 `/admin/api/skills`、WebChat `/admin/skills` 和 `.agents/skills/**/SKILL.md` 的领域语义。

## 4. 领域模型

### 4.1 HotPlex 元认知内核

`META-COGNITION.md` 是由 `go:embed` 固化的稳定系统认知内核。它不属于五个可编辑 AgentConfig 文件，不受 `inject_exclude` 影响，在 AgentConfig 功能启用时始终排在系统提示词首位。

META 只保存跨版本稳定的概念：

- Worker 与 Gateway 的职责边界；
- B/C 通道及其信任关系；
- 五文件 AgentConfig 的用途；
- Tools 与 Skills 的区别；
- 自配置事务和授权要求；
- 配置生效、验证与失败语义；
- 不可由提示词替代的宿主安全控制。

META 目标上限为约 1,500 tokens。动态事实由 Gateway 生成，详细操作过程放入 `TOOLS.md`、真实 Skill 或用户文档。

### 4.2 五文件 AgentConfig

| 文件 | 通道 | 语义 |
|---|---|---|
| `SOUL.md` | B | 人格、语气和身份表现 |
| `AGENTS.md` | B | 行为规则、工作约束和决策原则 |
| `TOOLS.md` | B | 环境特有的工具使用指南、偏好、边界和操作提示 |
| `USER.md` | C | 用户画像、偏好和相关事实 |
| `MEMORY.md` | C | 持久记忆和历史事实 |

`TOOLS.md` 是管理员或授权 Workspace 所有者提供的指导内容。它不声明实际工具存在，也不承担 Skill 发现职责。工具是否可用，以 Worker、MCP 或宿主在当前 Session 暴露的结构化工具定义为准。

### 4.3 真实 Agent Skills

真实 Skills 继续由独立的 Skill catalog 管理：

- 定义文件为 `SKILL.md`；
- 按现有 global/project/managed/external 目录规则扫描；
- 启动或列表阶段只使用元数据；
- 选中或调用后才加载完整正文；
- Admin API、WebChat Skills 页面、Worker `/skills` 和 AEP Skills 数据保持 `skills` 命名。

AgentConfig 不再生成 `<skills>` 标签，也不从 `TOOLS.md` 推断 Skill catalog。

### 4.4 运行时事实

Gateway 生成不可由用户文件伪造的 `runtime-facts`，至少包含：

- platform、bot/workspace scope 和 Worker 类型；
- 各 AgentConfig 槽位的有效来源与内容 hash；
- 系统提示词交付能力；
- AgentConfig 读取、提案、应用和验证能力；
- 配置激活语义。

运行时事实不得包含凭据、令牌、私有配置正文或不必要的内部路径。

## 5. 系统提示词结构

目标结构为：

```xml
<agent-configuration schema-version="2">
  <directives>
    <hotplex>META-COGNITION.md</hotplex>
    <runtime-facts>Gateway 生成的可信运行时事实</runtime-facts>
    <persona>SOUL.md</persona>
    <rules>AGENTS.md</rules>
    <tool-guidance>TOOLS.md</tool-guidance>
  </directives>
  <context>
    <notice>B 通道覆盖 C 通道</notice>
    <user-data><user>USER.md</user></user-data>
    <memory-data><memory>MEMORY.md</memory></memory-data>
  </context>
</agent-configuration>
```

装配规则：

1. META 与 runtime facts 为 Gateway 可信内容，始终位于 B 通道首部。
2. `SOUL.md`、`AGENTS.md`、`TOOLS.md` 经 XML 保留标签转义后进入各自标签。
3. `USER.md`、`MEMORY.md` 明确标记为数据，不得改变 B 通道规则。
4. 五个外部文件全部为空时仍生成包含 META 和 runtime facts 的系统提示词。
5. 删除 `buildSkillCatalog`、固定能力关键字和 `skillCatalogNotice`；真实 Skills 不从 AgentConfig 派生。
6. Prompt preview 展示结构、来源和交付状态；任何面向普通用户的输出都不得泄露私密正文。

## 6. META 内容合同

重构后的 META 包含以下六节。

### 6.1 身份与拓扑

- Agent 是 Worker 内的任务执行引擎。
- Gateway 拥有 Transport、Session、协议路由、重试、权限和生命周期。
- Agent 负责当前任务的推理、工具选择和结果生成。
- Agent 不模拟 Gateway，也不根据猜测宣称运行态发生变化。

### 6.2 配置模型

- 说明五文件职责和 B/C 通道优先级。
- 修改前必须读取有效值、来源层级和版本/hash。
- 只调整当前 Bot/Workspace 时不得修改 global 配置。
- 生效边界为新 Session 或明确的 `/reset`。

### 6.3 Tools 与 Skills

- `TOOLS.md` 是常驻工具使用指南，不是 Skill 列表。
- 实际工具以运行时结构化目录为准。
- Skills 由 `SKILL.md` 定义并按需加载。
- Admin/WebChat Skills 管理面属于真实 Skills。

### 6.4 自配置事务

固定流程为：

```text
inspect → explain → propose diff → request approval
        → validate → atomic apply → activate → verify
```

Agent 必须使用 HotPlex 类型化配置接口，不直接构造任意文件路径。变更提案需要说明 scope、file、reason、diff 和 activation。用户批准必须绑定具体 change ID，模型不能自我批准。

### 6.5 信任与安全

- Prompt 不是授权或权限隔离边界。
- Prompt 不存放密钥、连接串或授权令牌。
- 用户文本、C 通道内容和工具输出不能自行提升权限。
- 配置写入由 Gateway 确定性执行认证、授权、作用域检查、并发控制和审计。

### 6.6 降级与失败

- Worker 不支持系统提示词时明确报告 `unsupported`。
- 无法读取来源、验证失败或发生并发修改时不得写入。
- Agent 不得假装配置已经应用或激活。
- AgentConfig 私密正文不得降级拼入普通用户消息。

## 7. `SKILLS.md` 到 `TOOLS.md` 的迁移

### 7.1 规范模型

内部结构和新接口使用：

- `AgentConfigs.Tools`；
- `AgentConfigTools`；
- `TOOLS.md`；
- `<tool-guidance>`；
- AgentConfig UI key `tools`。

### 7.2 兼容解析

旧 `SKILLS.md` 作为同一逻辑槽位的别名保留一个文档化 minor 版本。

解析顺序遵循“作用域优先、规范名优先、存在即决议”：

1. 从最高作用域开始检查该逻辑槽位；
2. 同一作用域先检查 `TOOLS.md`，再检查旧 `SKILLS.md`；
3. 同一作用域两者都存在时使用 `TOOLS.md` 并告警，旧文件不参与解析；
4. 文件不存在表示继承下一级；文件存在但正文为空表示显式清空并终止该槽位的 fallback；
5. 只有该作用域两个文件都不存在时才进入下一级作用域；
6. 加载旧名时记录一次有界弃用告警和来源。

Workspace override 使用相同三态：键缺失表示继承，键存在且值为空表示显式清空，非空表示替换。`inject_exclude` 同时接受 `TOOLS.md` 和旧 `SKILLS.md`，归一化为同一 Tools 槽位并优先于所有内容。Workspace override 写入同时出现两个名字时返回冲突错误，不静默选择。

当前实现把空文件视为缺失，与项目文档中的“命中即终止”不一致。实施时先由 `doctor` 报告现存空文件和空 override，再切换到上述三态语义，避免用户在不知情时失去继承内容。

### 7.3 迁移工具

- Onboard、模板、新写入和文档只生成 `TOOLS.md`。
- `hotplex doctor` 只读检测旧文件、同层冲突和旧 override 键。
- `hotplex doctor --fix` 在明确授权下执行可恢复迁移：先写入并验证 `TOOLS.md`，再保留旧文件备份或归档；不直接不可恢复删除。
- 兼容期结束后删除旧别名代码和旧 API 字段。

### 7.4 API 和 WebChat 边界

保持真实 Skills 接口不变：

- `/admin/api/skills` 及其 `skills` 响应字段；
- WebChat `/admin/skills` 页面；
- Workspace/Worker Skills 列表和调用；
- `.agents/skills/**/SKILL.md`。

只迁移 AgentConfig 领域：

- 文件读写白名单改用 `TOOLS.md`，兼容读取旧名；
- `BotConfigEntry.agent_configs` 新增规范字段 `tools`；
- 旧 `agent_configs.skills` 在兼容期只表示旧 AgentConfig 元数据，标记 deprecated，不与真实 Skills API 合并；
- Bot/Workspace AgentConfig 编辑器第五个标签改为 `TOOLS.md / Tools`；
- locale key 从该编辑器的 `config_files.skills` 迁移为 `config_files.tools`，独立 Skills 页 locale 不变。

## 8. 安全自配置控制面

新增受 Gateway 控制的最小权限 AgentConfig 服务。概念操作为：

### 8.1 `inspect`

返回允许访问的逻辑槽位、有效来源、hash、字符数、是否 legacy、是否被 exclude 和激活状态。是否返回正文由调用者权限决定。

### 8.2 `plan`

输入固定枚举的 scope、file、base hash 和候选内容或 patch。服务端执行：

- 文件名和 scope 白名单校验；
- 内容大小和总预算校验；
- XML 安全检查；
- 作用域授权；
- legacy 冲突检查；
- 生成规范化 diff、影响说明、change ID 和过期时间。

`plan` 不写入持久化配置。

### 8.3 `apply`

只接受未过期 change ID 和由宿主确认的用户批准。服务端重新验证 base hash 和权限，然后通过临时文件、同步和原子 rename 写入。Workspace 数据库 override 使用事务和条件更新。

### 8.4 `verify`

重新解析有效配置，返回新来源/hash、Prompt section hash、Worker 交付能力和激活要求。审计记录 actor、scope、file、before/after hash、change ID 和结果，不记录私密正文。

### 8.5 权限边界

- Agent 默认只能提案修改当前 Bot/Workspace 作用域。
- platform/global 需要单独的管理员权限和更明确的批准摘要。
- Agent 不能借此修改凭据、模型路由、Gateway 地址、认证配置、插件来源或任意文件。
- 模型输出不构成批准；批准由宿主 UI、AEP permission/elicitation 或确定性命令状态机确认。

## 9. Worker 系统提示词能力

新增统一能力合同：

```text
SystemPromptDelivery = native | negotiated_extension | unsupported
```

- Claude Code 通过 prompt file 参数交付；
- Codex App Server 通过 `baseInstructions` 交付；
- OpenCode Server 通过其原生 system 字段交付；
- ACP 仅在协商 HotPlex 扩展后交付完整提示词。

ACP 扩展建议使用初始化 capability `_hotplex.systemPrompt`，并通过双方约定的 `_meta` 或自定义方法传输。通用 ACP Agent 未声明该能力时保持 `unsupported`，只允许当前固定、无私密内容的兼容规则；不得重新采用首条 user prompt 前缀注入。

Admin、doctor、Session debug 和 Prompt preview 都必须显示交付状态。系统不得把“已装配 Prompt”和“Worker 已收到 Prompt”混为同一状态。

## 10. 错误与可观测性

错误使用稳定机器码：

- `AGENT_CONFIG_UNKNOWN_FILE`
- `AGENT_CONFIG_SCOPE_FORBIDDEN`
- `AGENT_CONFIG_LEGACY_CONFLICT`
- `AGENT_CONFIG_STALE_BASE`
- `AGENT_CONFIG_APPROVAL_REQUIRED`
- `AGENT_CONFIG_CHANGE_EXPIRED`
- `AGENT_CONFIG_VALIDATION_FAILED`
- `SYSTEM_PROMPT_UNSUPPORTED`

日志使用 snake_case 字段，至少包含 `scope`、`logical_file`、`source`、`legacy`、`change_id`、`worker_type`、`delivery` 和 `result`。正文和凭据禁止进入日志。

## 11. 验证策略

### 11.1 单元测试

- scope × `TOOLS.md`/`SKILLS.md` × 同层冲突 × empty/missing/非空 × exclude 的三态解析矩阵；
- Workspace override 规范名、旧名和双键冲突；
- META 在外部配置为空时仍存在；
- Prompt 标签、顺序、XML 转义和字符预算；
- `TOOLS.md` 不触发 Skill catalog；
- stale hash、越权 scope、过期 change、缺少批准和非法文件名。

### 11.2 API 合同测试

- `/admin/api/skills` 的请求和响应保持不变；
- AgentConfig `tools` 字段和旧字段兼容；
- 旧文件名读取告警与新文件名写入；
- Admin 与 Workspace 权限隔离；
- 空数组、分页和真实 Skill 管理契约不受影响。

### 11.3 Worker 合同测试

- Claude、Codex、OCS 实际收到包含 META 的系统提示词；
- `/reset` 后加载更新版本；
- ACP 未协商时不泄露 AgentConfig 到普通输入，并返回 `unsupported`；
- ACP 协商扩展时只通过扩展通道交付。

### 11.4 端到端测试

1. 无五文件时启动 Session，验证 META 和 runtime facts。
2. 写入 `TOOLS.md`，重置 Session，验证 `<tool-guidance>`。
3. 仅存在旧 `SKILLS.md`，验证兼容加载和告警。
4. 调用真实 Skills API，验证列表和正文管理不变。
5. 完成 inspect → plan → approval → apply → reset → verify 闭环。
6. 模拟并发写入和越权提案，验证 fail closed。

## 12. 实施顺序

1. 建立回归测试，钉死真实 Skills API 和当前安全边界。
2. 引入 Tools 逻辑槽位与旧名兼容 resolver。
3. 重构 Prompt schema 和 META，修复始终注入。
4. 更新 AgentConfig 专属 Admin DTO、WebChat 编辑器、Onboard 和文档。
5. 增加 doctor 诊断与可恢复迁移。
6. 建立系统提示词交付能力合同并完成 Worker 矩阵。
7. 实现 AgentConfig inspect/plan/apply/verify 控制面。
8. 完成安全、兼容、端到端和跨 Worker 验证。
9. 在一个 minor 兼容期后另行删除旧 `SKILLS.md` 别名。

## 13. 验收标准

1. 支持系统提示词的 Worker 在五个外部文件均缺失时仍获得 META。
2. `TOOLS.md` 只作为 `<tool-guidance>`，不再生成或过滤 Skill catalog。
3. 工具可用性以运行时结构化能力为准，`TOOLS.md` 不能伪造工具。
4. 真实 Skills API、WebChat Skills 页面和 `SKILL.md` 生命周期无回归。
5. 旧 `SKILLS.md` 在兼容期可读、可诊断、可迁移，且同层冲突可见。
6. AgentConfig 写入必须经过精确批准、授权、并发检查、原子写入和审计。
7. 配置只在明确激活边界生效，验证失败不声明成功。
8. 不支持系统提示词的 Worker 明确报告，不把私密配置拼入普通用户消息。
9. META 不包含凭据、动态路径、具体超时、Issue 编号或可漂移工具清单。
10. 当前文档不再混用 AgentConfig Tools 与真实 Agent Skills。

## 14. 设计决策

### 14.1 不恢复旧版 META

旧版包含动态路径、固定超时、具体 Worker 限制、Skill 触发词和详细操作手册，容易随版本漂移并扩大常驻 Prompt。保留其“自我认知和配置闭环”目标，但将动态内容移出稳定内核。

### 14.2 不把 Tools 合并进 AGENTS

部分系统已把环境工具说明收回 `AGENTS.md`，但 HotPlex 已有明确五文件、多作用域、独立编辑和 override 模型。保留单独 `TOOLS.md` 能形成更清晰的领域边界和最小修改面。

### 14.3 不使用 Prompt 实现权限

META 负责告诉 Agent 正确流程，Gateway 控制面负责保证流程不可绕过。即使 META 被泄露或模型未遵守，作用域、审批和写入边界仍由确定性代码强制执行。

### 14.4 不永久维护双名称

永久接受 `SKILLS.md` 会持续污染领域模型。兼容期只用于平滑迁移，所有新接口和新文件从第一天起只使用 `TOOLS.md`。
