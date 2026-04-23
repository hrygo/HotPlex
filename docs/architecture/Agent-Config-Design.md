---
type: design
tags:
  - design/agent-config
  - architecture/context-injection
  - reference/prompt-engineering
related:
  - Claude-Code-Context-Analysis.md
---

# HotPlex Agent Context 设定文件方案

> 基于 [[Claude-Code-Context-Analysis]] 研究 + OpenClaw SOUL.md/AGENTS.md/USER.md 体系分析，
> 设计 HotPlex 如何通过设定文件控制 Claude Code 的行为框架。

---

## 1. OpenClaw 设定文件体系研究

### 1.1 架构总览

OpenClaw 将 Agent 的 "人格 + 行为 + 记忆" 拆解为独立的 Markdown 文件，存放在 workspace 目录（默认 `~/.openclaw/workspace/`）中：

```
~/.openclaw/workspace/
├── AGENTS.md       ← 工作空间规则、行为红线、记忆策略
├── SOUL.md         ← 人格、语气、价值观、风格
├── IDENTITY.md     ← 名字、头像、自我认知
├── USER.md         ← 用户画像、偏好、时区
├── TOOLS.md        ← 本地工具配置笔记
├── BOOTSTRAP.md    ← 首次运行仪式 (完成后自动删除)
├── HEARTBEAT.md    ← 定时任务指令 (放在 cache boundary 下方)
└── MEMORY.md       ← 长期记忆 (仅主会话加载)
```

### 1.2 注入位置与优先级

OpenClaw 将这些文件注入到 **system prompt** 中（而非 messages），作为一个名为 `# Project Context` 的 section：

```
OpenClaw System Prompt 结构:

  [Tooling Section]          ← 硬编码工具说明
  [Safety Section]           ← 硬编码安全规范
  [Skills Section]           ← 技能目录
  [Memory Section]           ← 记忆检索指南

  # Project Context          ← 设定文件注入点 (cache boundary 上方)
  The following project context files have been loaded:
  If SOUL.md is present, embody its persona and tone.

  ## AGENTS.md              ← priority 10
  [内容...]

  ## SOUL.md                ← priority 20
  [内容...]

  ## IDENTITY.md            ← priority 30
  [内容...]

  ## USER.md                ← priority 40
  [内容...]

  ## TOOLS.md               ← priority 50
  [内容...]

  ## MEMORY.md              ← priority 70
  [内容...]

  ─── CACHE BOUNDARY ───    ← 缓存分界线

  # Dynamic Project Context  ← 动态内容 (cache boundary 下方)
  ## HEARTBEAT.md
  [内容...]

  ## Runtime
  Model: ..., OS: ..., Shell: ...
```

### 1.3 关键设计特征

| 特征 | OpenClaw 做法 | 效果 |
|------|--------------|------|
| **注入位置** | system prompt 内，`# Project Context` section | 高优先级，直接塑造模型行为 |
| **SOUL.md 特殊处理** | 额外注入 "embody its persona and tone" 指令 | 人格指令得到强化 |
| **缓存分界线** | 静态文件在 boundary 上方，HEARTBEAT.md 在下方 | 稳定内容可缓存，动态内容不破坏缓存 |
| **子 Agent 裁剪** | subagent 只加载 AGENTS.md + TOOLS.md + SOUL.md + IDENTITY.md + USER.md | 节省 token，避免子 agent 看到 MEMORY |
| **文件大小限制** | 单文件 12K chars，总计 60K chars | 防止 context 爆炸 |
| **MEMORY 隔离** | MEMORY.md 仅主会话加载，不在群聊/共享会话加载 | 防止隐私泄漏 |
| **排序机制** | `CONTEXT_FILE_ORDER` Map 定义数字优先级 | 确定性顺序，避免随机性 |
| **frontmatter 剥离** | YAML frontmatter 加载时 strip | 元数据不注入到 prompt |

### 1.4 与 Claude Code 的对比

| 维度 | Claude Code | OpenClaw |
|------|-------------|----------|
| **注入目标** | `messages[]` 头部<br>`<system-reminder>` 包裹<br>"may or may not be relevant" **(削弱)** | system prompt 内<br>直接作为 system section<br>"embody its persona" **(强化)** |
| **行为规范** | 硬编码在 S2 static<br>用户只能在 messages 层<br>尝试覆盖 (被削弱) | `AGENTS.md` (用户可编辑)<br>红线/权限/记忆策略都可定制 |
| **文件粒度** | `CLAUDE.md` (全合一)<br>一个文件承载所有内容 | 按职责拆分<br>SOUL / AGENTS / USER / IDENTITY / TOOLS / MEMORY |

---

## 2. 注入通道特性分析

### 2.1 通道 B 与通道 C 的关键差异

源码级验证 (`src/utils/systemPrompt.ts`, `src/utils/api.ts`, `src/utils/claudemd.ts`):

| 维度 | 通道 B (`--append-system-prompt`) | 通道 C (`.claude/rules/*.md`) |
|------|-----------------------------------|-------------------------------|
| **注入位置** | `system[]` S3 尾部 | `messages[]` M0 头部 |
| **API 参数位置** | `system` parameter | `messages` parameter |
| **语义角色** | "开发者指令" | "可能不相关的上下文" |
| **模型理解** | 必须遵循的指令 | 可选择应用的参考信息 |
| **削弱声明** | 无 | `"may or may not be relevant"` |
| **覆盖 S2 能力** | 有效 (S3 尾部 > S2 前部) | 被削弱 (hedging 降低优先级) |
| **多轮稳定性** | 每轮重新注入 | compact 后重新注入 |
| **缓存** | `null` (每轮全额重发) | messages cache (~90% 折扣) |
| **实现方式** | CLI 参数 | 文件写入 workdir |

### 2.2 S1 前缀自动切换

当使用 `--append-system-prompt` 时，Claude Code 自动将 S1 前缀从默认值切换为 SDK 模式 (`src/constants/system.ts:39-43`):

```
默认:      "You are Claude Code, Anthropic's official CLI for Claude."
+ append:  "You are Claude Code, ..., running within the Claude Agent SDK."

模型已知自己运行在 SDK 环境中 → 更容易接受外部注入的人格和规则。
```

---

## 3. B+C 组合方案设计

### 3.1 分配原则：按注入位置效果决定归属

```
核心判断标准 (按优先级排序):

  ① 是否需要覆盖 S2 硬编码默认值？
     S2 规定了: 无注释 / 简短输出 / 专用工具优先
     项目规则可能与 S2 冲突 → 必须进 B (system level) 才能有效覆盖

  ② 被削弱后果的严重程度？
     "may not be relevant" 削弱 C 中内容 → 评估每种内容的被削弱后果

  ③ 内容性质：行为指令 vs 上下文数据？
     "你必须怎样" → B (指令性)
     "这是相关信息" → C (事实性)
```

### 3.2 逐内容评估

| 内容 | 需覆盖 S2? | 被削弱后果 | 性质 | 结论 |
|------|-----------|-----------|------|------|
| **SOUL.md** 人格 | ✅ 覆盖 S1<br>"You are Claude Code" 身份声明 | 🔴 人格丧失<br>Agent 退化为通用助手 | 行为指令<br>非可选上下文 | **→ B** 必须强制 |
| **AGENTS.md** 工作规则 | ✅ 覆盖 S2<br>注释风格 / 输出详细度 / 工具偏好 / 反模式清单 | 🔴 规则失效<br>项目要求 doc comment 但 S2 说无注释 → 模型倾向 S2 | 行为指令<br>非可选上下文 | **→ B** 必须强制 |
| **SKILLS.md** 工具指南 | ✅ 覆盖 S2<br>"Prefer dedicated tools" | 🟡 平台行为<br>次优但可用 | 行为指引<br>偏操作指导 | **→ B** 强烈建议 |
| **USER.md** 用户画像 | ❌ 不冲突<br>纯增量信息 | 🟢 回复风格稍不匹配<br>但功能正常 | 上下文数据<br>偏好参考 | **→ C** 信息足够 |
| **MEMORY.md** 持久记忆 | ❌ 不冲突<br>纯增量信息 | 🟢 遗忘偏好<br>可下次学习 | 上下文数据<br>历史记录 | **→ C** 信息足够 |

### 3.3 最终分配方案

```
┌──────────────────────────────────────────────────────────────────────────┐
│ 通道 B (--append-system-prompt)     ~3.5K tokens                       │
│ "必须遵循的行为框架" — system role，无削弱声明                         │
│                                                                         │
│  # Agent Persona                     ← SOUL.md (~500 tok)              │
│  If SOUL.md is present, embody its persona and tone.                   │
│  Follow its guidance unless higher-priority instructions override it.   │
│  Avoid stiff, generic replies.                                          │
│  [人格/语气/价值观/红线...]                                             │
│                                                                         │
│  # Workspace Rules                   ← AGENTS.md (~2K tok)             │
│  [自主行为边界/确认策略/反模式清单/记忆策略/工具偏好...]                 │
│                                                                         │
│  # Tool Usage Guide                  ← SKILLS.md (~1K tok)             │
│  [消息平台操作指南/STT 配置/构建命令/...]                               │
│                                                                         │
│  效果: 三个 section 形成完整行为框架                                     │
│        SOUL (我是谁) → AGENTS (我怎么做) → SKILLS (我用什么工具)         │
│        全部以 "开发者指令" 语义送达，覆盖 S1 身份 + S2 默认行为          │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ 通道 C (workdir/.claude/rules/hotplex-*.md)                            │
│ "参考上下文" — system-reminder，带 hedging 声明                        │
│                                                                         │
│  hotplex-user.md                    ← USER.md                          │
│  [称呼/角色/时区/技术背景/沟通偏好/...]                                 │
│                                                                         │
│  hotplex-memory.md                  ← MEMORY.md                        │
│  [跨会话记忆/反馈纠正/项目上下文/...]                                   │
│                                                                         │
│  (现有项目 CLAUDE.md 保持不动)                                          │
│                                                                         │
│  效果: 上下文数据以 "项目参考" 语义送达                                  │
│        不与 B 中的行为指令竞争注意力                                     │
│        B = 指令性, C = 事实性 → 语义分层正确                            │
└──────────────────────────────────────────────────────────────────────────┘
```

### 3.4 语义分层总览

```
注入位置效果 (从强到弱):

  S2 静态硬编码        "Be careful with security"           基线安全网
  S3 B-行为框架        "MUST follow these rules"  ← HotPlex  项目强制规则
  S3 B-人格            "embody its persona"       ← HotPlex  Agent 身份
  ─── 注意力分界 ───
  M0 C-项目知识        "Here's the codebase map"             参考信息
  M0 C-用户画像        "User prefers concise replies"       参考信息
  M0 C-记忆            "Last time we decided X"             参考信息

B 和 C 不竞争同一类注意力:
  B = "你必须怎样" (指令性) → 模型作为规则理解
  C = "这是相关信息" (事实性) → 模型作为上下文理解
```

---

## 4. C 通道注入机制

### 4.1 Claude Code 的 .claude/rules/ 自动发现

源码确认 (`src/utils/claudemd.ts:910-918`):

```typescript
// Claude Code 自动扫描 workdir/.claude/rules/ 下所有 .md 文件
const rulesDir = join(dir, '.claude', 'rules')
result.push(
  ...(await processMdRules({
    rulesDir,
    type: 'Project',
    conditionalRule: false,  // 无 frontmatter = 无条件全局生效
  })),
)
```

- 无 frontmatter 的 `.md` 文件全局生效
- 不需要额外 CLI 参数或环境变量
- 不干扰现有 CLAUDE.md 和 rules 文件
- Claude Code 自动合并到 M0 User Context 的 `<system-reminder>` 中

### 4.2 C 通道注入流程

```
Step 1: Worker 启动前 — 写入 rules 文件

  ~/.hotplex/agent-configs/USER.md
    → strip frontmatter
    → workdir/.claude/rules/hotplex-user.md

  ~/.hotplex/agent-configs/MEMORY.md
    → strip frontmatter
    → workdir/.claude/rules/hotplex-memory.md


Step 2: 启动 Claude Code 子进程

  claude \
    --append-system-prompt "$(buildAppendPrompt)" \
    --permission-mode auto \
    --model claude-sonnet-4-6

  Claude Code 自动发现 .claude/rules/hotplex-*.md → 注入 M0


Step 3: Worker 会话结束 — 可选清理

  rm workdir/.claude/rules/hotplex-*.md
  (或保留复用，内容会话间通常不变)


M0 User Context 最终结构:

  # claudeMd
  ## ~/.claude/CLAUDE.md               ← 用户全局指令 (不动)
  ## workdir/CLAUDE.md                 ← 项目 CLAUDE.md (不动)
  ## workdir/.claude/rules/linting.md  ← 项目现有规则 (不动)
  ## workdir/.claude/rules/hotplex-user.md    ← USER.md
  ## workdir/.claude/rules/hotplex-memory.md  ← MEMORY.md
  # currentDate: 2026/04/23
  IMPORTANT: this context may or may not be relevant...
```

### 4.3 实现代码

```go
// internal/messaging/agent_config.go

const hotplexRulesPrefix = "hotplex-"

// InjectCRules writes C-channel content to workdir/.claude/rules/
func InjectCRules(workdir string, configs *AgentConfigs) error {
    rulesDir := filepath.Join(workdir, ".claude", "rules")
    if err := os.MkdirAll(rulesDir, 0755); err != nil {
        return fmt.Errorf("mkdir rules: %w", err)
    }

    files := map[string]string{
        "hotplex-user.md":   configs.User,
        "hotplex-memory.md": configs.Memory,
    }
    for name, content := range files {
        if content == "" {
            continue
        }
        content = stripYAMLFrontmatter(content)
        if err := os.WriteFile(filepath.Join(rulesDir, name), []byte(content), 0644); err != nil {
            return fmt.Errorf("write %s: %w", name, err)
        }
    }
    return nil
}

// CleanupCRules removes HotPlex rule files from workdir
func CleanupCRules(workdir string) error {
    rulesDir := filepath.Join(workdir, ".claude", "rules")
    matches, err := filepath.Glob(filepath.Join(rulesDir, hotplexRulesPrefix+"*.md"))
    if err != nil {
        return err
    }
    for _, f := range matches {
        if err := os.Remove(f); err != nil {
            log.Warn("failed to remove rule file", "path", f, "error", err)
        }
    }
    return nil
}
```

---

## 5. 完整 Context 组装流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    HotPlex → Claude Code Context 注入流程                   │
└─────────────────────────────────────────────────────────────────────────────┘

  Step 1: 加载设定文件
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  ~/.hotplex/agent-configs/                                              │
  │  ├── SOUL.md    → soulContent     (人格/语气/价值观)       → B        │
  │  ├── AGENTS.md  → agentsContent   (工作规则/红线/记忆策略) → B        │
  │  ├── SKILLS.md  → skillsContent   (工具使用指南)           → B        │
  │  ├── USER.md    → userContent     (用户画像/偏好/时区)     → C        │
  │  └── MEMORY.md  → memoryContent   (跨会话记忆)             → C        │
  │                                                                         │
  │  加载规则:                                                              │
  │  · 按平台选择变体: SOUL.slack.md > SOUL.md (优先平台特定版本)          │
  │  · frontmatter (YAML) 剥离后注入                                       │
  │  · 单文件上限 12K chars，总计上限 60K chars                             │
  │  · 文件不存在 → 跳过 (不报错)                                          │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 2: 组装 B 通道 (--append-system-prompt)
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  func buildAppendSystemPrompt(soul, agents, skills string) string {     │
  │    parts := []string{}                                                  │
  │                                                                         │
  │    if soul != "" {                                                      │
  │      parts = append(parts, fmt.Sprintf(`# Agent Persona                 │
  │  If SOUL.md is present, embody its persona and tone.                    │
  │  Follow its guidance unless higher-priority instructions                │
  │  override it. Avoid stiff, generic replies.                             │
  │                                                                         │
  │  %s`, soul))                                                            │
  │    }                                                                    │
  │                                                                         │
  │    if agents != "" {                                                    │
  │      parts = append(parts, "# Workspace Rules\n"+agents)               │
  │    }                                                                    │
  │                                                                         │
  │    if skills != "" {                                                    │
  │      parts = append(parts, "# Tool Usage Guide\n"+skills)              │
  │    }                                                                    │
  │                                                                         │
  │    return strings.Join(parts, "\n\n")                                   │
  │  }                                                                      │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 3: 注入 C 通道 (.claude/rules/)
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  InjectCRules(workdir, configs)                                         │
  │    → workdir/.claude/rules/hotplex-user.md   (USER.md)                 │
  │    → workdir/.claude/rules/hotplex-memory.md (MEMORY.md)               │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 4: 构建 Claude Code CLI 调用
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  claude \                                                               │
  │    --append-system-prompt "$APPEND_PROMPT" \                            │
  │    --permission-mode auto \                                             │
  │    --model claude-sonnet-4-6                                            │
  │                                                                         │
  │  APPEND_PROMPT = SOUL.md + AGENTS.md + SKILLS.md                       │
  │  .claude/rules/ = USER.md + MEMORY.md (Claude Code 自动发现)           │
  └─────────────────────────────────────────────────────────────────────────┘
```

---

## 6. 最终 Context 分布图

```
HotPlex 启动的 Claude Code 完整 Context 结构:

  system[] (System Prompt)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ S0  Attribution                      (不可控)                            │
  │ S1  CLI Prefix (SDK模式)             (不可控, 自动切换)                  │
  │ S2  Static Content (~15K tok)        (不可控, global cache)              │
  │     # System / # Doing Tasks / # Executing Actions / ...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S3  Dynamic Content                  (部分可控)                          │
  │     session_guidance / env_info / language / MCP instructions / ...     │
  │     ─────────────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex B 通道 (--append-system-prompt) ↓↓↓                    │
  │     ─────────────────────────────────────────────────────────────────── │
  │                                                                         │
  │     # Agent Persona                  ← SOUL.md (~500 tok)              │
  │     If SOUL.md is present, embody its persona and tone.                │
  │     [人格/语气/价值观/红线...]                                         │
  │                                                                         │
  │     # Workspace Rules                ← AGENTS.md (~2K tok)             │
  │     [自主行为边界/反模式/工具偏好/...]                                  │
  │                                                                         │
  │     # Tool Usage Guide               ← SKILLS.md (~1K tok)             │
  │     [消息平台操作/STT/构建命令/...]                                     │
  │                                                                         │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S4  System Context                   (不可控)                            │
  │     gitStatus / cacheBreaker                                            │
  └──────────────────────────────────────────────────────────────────────────┘

  messages[] (对话)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ M0  User Context <system-reminder>                                      │
  │     ─────────────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex C 通道 (.claude/rules/hotplex-*.md) ↓↓↓                │
  │     ─────────────────────────────────────────────────────────────────── │
  │                                                                         │
  │     ## workdir/CLAUDE.md             ← 项目知识 (不动)                  │
  │     Overview / Structure / Code Map / Conventions / Commands            │
  │                                                                         │
  │     ## workdir/.claude/rules/        ← 项目现有规则 (不动)              │
  │                                                                         │
  │     ## workdir/.claude/rules/hotplex-user.md   ← USER.md               │
  │     [称呼/角色/时区/偏好/沟通风格/...]                                  │
  │                                                                         │
  │     ## workdir/.claude/rules/hotplex-memory.md ← MEMORY.md             │
  │     [跨会话记忆/反馈纠正/项目上下文/...]                                │
  │                                                                         │
  │     currentDate: 2026/04/23                                             │
  │     IMPORTANT: this context may or may not be relevant...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ M1  Deferred Tools                                                      │
  │ M2  Session Start                                                       │
  │ M3+ Conversation History                                                │
  └──────────────────────────────────────────────────────────────────────────┘
```

---

## 7. 设定文件模板

### 7.1 SOUL.md — Agent 人格 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex Agent 人格定义"
---

# SOUL.md - Agent 人格

## 身份

你是 HotPlex 团队的 AI 软件工程搭档，专注于 {{project_type}} 领域。

## 核心特质

- **主动思考**: 不只是执行指令，而是像资深同事一样提出假设和风险预警
- **技术敏感**: 关注 SOTA 技术，主动识别技术债务和安全风险
- **务实高效**: 语义理解优先，DRY & SOLID，异常路径全覆盖

## 沟通风格

- 语言: {{language}} 交流，技术术语保留英文
- 格式: Markdown 结构化，简洁直接
- 风格: 像资深同事协作，不是被动执行器
- 边界: 不确定时提出假设而非猜测

## 价值观

- 代码质量 > 开发速度 (但不过度工程化)
- 安全 > 便利 (OWASP Top 10 零容忍)
- 可观测性 > 静默运行
- 用户意图 > 字面指令 (理解 WHY)

## 红线

- 绝不泄露 API key、token、密码等敏感信息
- 绝不执行未经确认的 destructive 操作
- 绝不向外部服务发送未审查的敏感数据
- 遇到安全漏洞立即修复，不推迟
```

### 7.2 AGENTS.md — 工作空间规则 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex 工作空间行为规范"
---

# AGENTS.md - 工作规则

## 自主行为边界

**✅ 无需确认即可执行:**
- 读取/搜索/分析文件
- 运行测试/lint/构建
- Git commit/branch 操作
- 自动修复 lint 错误

**⚠️ 需要确认:**
- 首次方案设计
- 删除操作
- 依赖变更
- 远程推送 (git push)
- 外部服务调用

**❌ 绝对禁止:**
- 直接 push main/master 分支
- rm -rf 等破坏性操作
- 泄露敏感信息

## 记忆策略

- 用户明确要求 "记住" → 写入 MEMORY.md
- 修正错误行为 → 写入 MEMORY.md 反馈区
- 每次会话开始 → 隐式读取 MEMORY.md
- 用户说 "忘记" → 从 MEMORY.md 移除

## 工具使用偏好

| 任务 | 首选工具 |
|:-----|:---------|
| 探索代码库 | Task(Explore) |
| 查找文件 | Glob |
| 搜索内容 | Grep |
| 读取文件 | Read |
| 编辑文件 | Edit |

## 反模式

- ❌ `sync.Mutex` 嵌入或指针传递 — 显式 `mu` 字段
- ❌ `math/rand` 用于加密 — 使用 `crypto/rand`
- ❌ Shell 执行 — 仅允许 `claude` 二进制
- ❌ 跳过 WAL mode 的 SQLite
```

### 7.3 SKILLS.md — 工具使用指南 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex 工具使用指南"
---

# SKILLS.md - 工具使用指南

## 消息平台

### Slack
- 流式输出 → 150ms flush 间隔, 20-rune 阈值
- 长消息 → chunker 分割 → dedup 去重 → rate limiter → send
- 图片 → image block rendering + file upload
- 状态更新 → StatusManager threadState 管理

### 飞书
- 流式卡片 → 4 层防御: TTL → integrity → retry → IM Patch fallback
- 语音消息 → STT 转写 (SenseVoice-Small ONNX)
- 交互卡片 → InteractionManager 权限请求

## STT 语音转文字

- 引擎: SenseVoice-Small via funasr-onnx (ONNX FP32)
- 模式: PersistentSTT (长驻子进程, JSON-over-stdio)
- 配置: stt_provider / stt_local_cmd / stt_local_mode

## 构建/测试

```bash
make build          # 构建
make test           # 测试 (含 -race)
make lint           # golangci-lint
make check          # 完整 CI: fmt + vet + lint + test + build
```
```

### 7.4 USER.md — 用户画像 (→ C 通道)

```markdown
---
version: 1
description: "HotPlex 用户画像"
---

# USER.md - 用户画像

## 基本信息

- **称呼**: {{user_name}}
- **角色**: {{user_role}}
- **时区**: {{user_timezone}}
- **语言偏好**: {{language}}

## 技术背景

- **主要语言**: Go, TypeScript
- **框架经验**: React, Echo, Gin
- **基础设施**: Docker, Kubernetes, PostgreSQL

## 工作偏好

- 喜欢原子提交 + Conventional Commits
- 偏好代码审查式反馈 (指出问题 + 建议方案)
- 不喜欢过度解释基础概念
- 多任务时使用 TODO LIST 追踪

## 沟通偏好

- 简短直接，不要总结已完成的工作
- 使用 file:line 格式引用代码
- 技术决策需要说明 WHY
- 不确定时直接说 "需要调查"
```

---

## 8. 实施路径

### 8.1 阶段规划

```
Phase 1: 最小可用 (1-2 天)
├── 实现 agent-configs/ 目录的文件加载器
├── 实现 SOUL.md → --append-system-prompt 组装 (含 "embody persona" 强化)
├── 实现 AGENTS.md + SKILLS.md → append-system-prompt 追加
├── 实现 USER.md + MEMORY.md → .claude/rules/ 注入
└── 在 Worker 启动流程中集成

Phase 2: 完善机制 (3-5 天)
├── 添加 frontmatter 解析与验证
├── 添加文件大小限制 (12K chars / 60K total)
├── 添加 session 级缓存 (会话内只加载一次)
├── 子 Agent 场景裁剪 (仅加载 SOUL.md + AGENTS.md, 不加载 MEMORY)
└── Worker 会话结束时清理 .claude/rules/hotplex-*.md

Phase 3: 动态能力 (1 周)
├── 按平台/通道动态选择配置变体
│   (SOUL.slack.md / SOUL.feishu.md / SOUL.cli.md)
│   (AGENTS.slack.md / AGENTS.feishu.md)
├── 运行时热更新 (文件变更 → 下次会话生效)
├── 用户画像自动学习 (从对话中提取偏好更新 USER.md)
└── MEMORY.md 自动管理 (类似 OpenClaw 的 daily log 压缩)
```

### 8.2 Worker 集成点

```
internal/worker/claudecode/worker.go 中的修改:

  Start() 方法:

  func (w *ClaudeCodeWorker) Start(ctx context.Context) error {
    // 1. 加载 agent configs
    configs := loadAgentConfigs(w.config.AgentConfigDir, platform)

    // 2. B 通道: 组装 --append-system-prompt
    appendPrompt := buildAppendSystemPrompt(
      configs.Soul,     // SOUL.md + "embody persona" 强化
      configs.Agents,   // AGENTS.md
      configs.Skills,   // SKILLS.md
    )

    // 3. C 通道: 注入 .claude/rules/
    if err := InjectCRules(w.workDir, configs); err != nil {
      log.Warn("inject C-rules failed", "error", err)
    }

    // 4. 构建命令参数
    args := []string{
      "--append-system-prompt", appendPrompt,
      // ... 现有参数
    }

    // 5. 启动 Claude Code 子进程
    // ...
  }

  Stop/Close 方法:
    // 清理 C 通道 rules 文件
    CleanupCRules(w.workDir)
```

### 8.3 配置结构

```go
// internal/config/config.go 新增

type AgentConfig struct {
    // Agent 配置文件目录 (默认: ~/.hotplex/agent-configs/)
    ConfigDir string `yaml:"config_dir" mapstructure:"config_dir"`

    // 各文件路径 (覆盖默认路径)
    SoulPath    string `yaml:"soul_path"    mapstructure:"soul_path"`
    AgentsPath  string `yaml:"agents_path"  mapstructure:"agents_path"`
    UserPath    string `yaml:"user_path"    mapstructure:"user_path"`
    SkillsPath  string `yaml:"skills_path"  mapstructure:"skills_path"`
    MemoryPath  string `yaml:"memory_path"  mapstructure:"memory_path"`

    // 大小限制
    MaxFileChars  int `yaml:"max_file_chars"  mapstructure:"max_file_chars"`   // 默认 12000
    MaxTotalChars int `yaml:"max_total_chars" mapstructure:"max_total_chars"`  // 默认 60000

    // C 通道清理策略
    CleanupRulesOnExit bool `yaml:"cleanup_rules_on_exit" mapstructure:"cleanup_rules_on_exit"` // 默认 false (保留复用)
}
```

### 8.4 按平台选择配置变体

```
~/.hotplex/agent-configs/
├── SOUL.md              ← 默认人格 (CLI / API 模式)
├── SOUL.slack.md        ← Slack 模式 (更简短, emoji 友好)
├── SOUL.feishu.md       ← 飞书模式 (正式, 企业场景)
├── AGENTS.md
├── AGENTS.slack.md      ← Slack 特定规则 (消息分割/去重/格式)
├── AGENTS.feishu.md     ← 飞书特定规则 (卡片/交互)
├── SKILLS.md
├── SKILLS.slack.md      ← Slack 工具指南
├── SKILLS.feishu.md     ← 飞书工具指南
├── USER.md
└── MEMORY.md

加载逻辑:
  func selectConfigFile(baseDir, baseName, platform string) string {
      if platform != "" {
          platformFile := filepath.Join(baseDir, baseName+"."+platform+".md")
          if fileExists(platformFile) {
              return platformFile
          }
      }
      return filepath.Join(baseDir, baseName+".md")
  }
```

---

## 9. 总结

**设计原则 (按优先级排序):**

1. **注入位置效果优先**
   需覆盖 S2 默认值的内容 → B (system, 无 hedging)；纯增量上下文内容 → C (messages, 有 hedging 但影响轻微)
   B = 行为框架 (SOUL+AGENTS+SKILLS), C = 上下文数据 (USER+MEMORY)

2. **语义分层**
   B 承载 "必须遵循" 的指令性内容 (system role)；C 承载 "参考信息" 的事实性内容 (user context role)
   两类内容不竞争注意力，模型分别作为规则和上下文理解

3. **职责分离** (借鉴 OpenClaw)
   SOUL (人格) / AGENTS (规则) / SKILLS (工具) / USER (用户) / MEMORY
   每个文件只承载一个关注点，便于独立维护和按平台选择变体

4. **非侵入式 C 通道**
   `.claude/rules/hotplex-*.md` 不修改现有项目文件
   `hotplex-*` 前缀确保精确清理，不误删项目 rules

5. **保留 Claude Code 默认能力**
   `--append-system-prompt` 而非 `--system-prompt`
   S2 的安全规范/工具指南/代码风格作为基线保留；B 通道内容补充 S2 未覆盖的项目特定规则

6. **平台适配**
   `SOUL.slack.md` / `SOUL.feishu.md` 按平台选择人格变体
   AGENTS/SKILLS 同理，不同平台不同规则和工具指南

7. **安全边界**
   文件大小限制 (12K / file, 60K total)；frontmatter 剥离 (元数据不注入 prompt)
   MEMORY.md 仅主会话加载 (防止隐私泄漏)；子 Agent 场景裁剪 (仅加载 SOUL + AGENTS, 跳过 MEMORY)
