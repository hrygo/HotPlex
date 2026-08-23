---
title: Skill 配置教程
weight: 7
description: 用 zip 上传管理 AI 技能 —— HotPlex Skill 发现、调用与显式同步
---

# Skill 配置教程

Skill 是封装了特定能力的真实 Agent Skill（含 `SKILL.md` 定义和辅助文件）。HotPlex 支持通过
WebChat/Admin 上传 zip 包安装，也会公开两个 embedded canonical package：runtime 的
`hotplex-cli`（Cron、显式 Slack、只读诊断）和需要显式 operator authority 的
`hotplex-operator`（服务、更新、配置、Admin、审计）。它们与 AgentConfig 的 `TOOLS.md`
是两个领域；`TOOLS.md` 不会出现在 Skills catalog。完整规格见
[Admin API Skill 管理参考](../reference/admin-api.md#skill-admin-)。

两个内置包以 `internal/skills/builtin/hotplex-cli` 和
`internal/skills/builtin/hotplex-operator` 为 canonical source，生成 byte-identical 的
`.agents/skills/hotplex-cli` 与 `.agents/skills/hotplex-operator` mirror。仓库 portfolio 还包含
`hotplex-diagnostics`、`hotplex-release`、`hotplex-docs-patrol`，合计五个 Skill。

真实 Skill 只按 `<skills-root>/<name>/SKILL.md` 发现。其他既有用户文件不会被识别，HotPlex 也
不会自动删除或改写它们；需要清理时必须由用户明确指定目标。

**前置条件**：HotPlex Gateway v1.37+ 已运行，WebChat 多租户已启用。

## 1. 两种 skill 范围

| 范围 | 存储位置 | 谁可管理 | 入口 |
|------|---------|---------|------|
| 全局 | `~/.agents/skills` | 管理员 | 管理后台 → Skills |
| Workspace | `<work_dir>/.agents/skills` | workspace owner | Workspace 设置 → Skills |

Workspace skill 同名会**覆盖**全局 skill 生效（UI 会给出 warning 提示遮蔽）。

## 2. 打包 skill zip

zip 包结构二选一（对齐 [agentskills.io](https://agentskills.io/specification) 标准）：

**扁平结构**（zip 根下直接 `SKILL.md`）：

```
my-skill.zip
└── SKILL.md
```

**单顶层目录**（目录名必须等于 frontmatter `name`）：

```
my-skill.zip
└── my-skill/
    ├── SKILL.md
    └── reference.md
```

`SKILL.md` 头部须有 YAML frontmatter：

```markdown
---
name: my-skill
description: 一句话描述这个技能的作用（1-1024 字符）
---

# My Skill

技能正文……
```

**格式约束**：

- `name`：正则 `^[a-z0-9]+(-[a-z0-9]+)*$`，1-64 字符；单顶层结构时必须等于父目录名
- `description`：1-1024 字符（必填，独立严格校验）
- 文件类型白名单：`.md`/`.json`/`.yaml`/`.yml`/`.txt`/`.py`/`.sh`/`.toml`/`.png`/`.jpg`/`.jpeg`/`.svg`；拒可执行/二进制
- 容量上限：zip ≤20MB、解压总 ≤50MB、单文件 ≤5MB、entry ≤500、压缩率 >100× 拒

## 3. 上传安装

WebChat UI → 管理后台（全局）或 Workspace 设置（workspace）→ Skills →「上传 Skill」→ 选择 zip → 勾选「覆盖同名」可替换已有同名 skill。

安装成功后，skill 立即出现在管理列表：`.agents/skills` 下的标注「可管理」，`.claude`/`.hotplex` 等目录下的标注「只读外部」。它是否能在某个 Session 中调用，仍取决于该 Session 的 Worker 目录证据。

## 4. 各 Worker 原生根与显式同步

HotPlex 不把 `$HOTPLEX_HOME` 的 inventory 当作 Worker 根，也不要求用户手工建立软链。同步
命令会在安全检查后把选定 package 投影到 UserHome 的原生目录：

| Worker | UserHome 原生根 | 说明 |
|--------|----------------|------|
| **Claude Code** | `<UserHome>/.claude/skills` | 当前 worker 的权威目录可证明 `callable` |
| **Codex CLI** | `<UserHome>/.agents/skills` | 与 OpenCode 共享根，选择任一 alias 会报告完整 aliases |
| **OpenCode Server** | `<UserHome>/.agents/skills` | 与 Codex 共享根，选择任一 alias 会报告完整 aliases |
| **ACP** | 无可推断 filesystem root | typed unsupported，不写入文件系统 |

使用以下命令查看、同步或移除 native projection：

```bash
hotplex skills status --profile runtime --worker claude_code --json
hotplex skills sync --profile runtime --worker claude_code --dry-run
hotplex skills sync --profile operator --worker codex_cli --worker opencode_server
hotplex skills remove --profile runtime --worker claude_code --json
```

`runtime` 只包含 `hotplex-cli`，`operator` 累积包含 `hotplex-cli` 与 `hotplex-operator`。
`status`/`--dry-run` 严格只读；未显式传 `--worker` 时，CLI 只从已启用 Slack/Feishu/Yuanxin
platform/bot 的 effective config 解析目标，空集合返回 bounded error，不回退到 RegisteredTypes。
UserHome 原生根与 `$HOTPLEX_HOME/skills/builtin/<version>` immutable inventory、状态和
receipts 分离。同步不会覆盖未知 user/project Skill；collision、drift、failed item 以非零
结果报告。`remove` 只删除有 matching receipt 且 unchanged-tree 能证明归属的 projection，
不会删除 immutable inventory。

## 5. 发现不等于可调用

Admin API 与 WebChat 的 public HTTP Skills catalog 管理或展示 Agent Skills；`TOOLS.md` 是
常驻指导，不会变成 Skill catalog。Session `/skills` 仍按当前
Worker/filesystem evidence 决定是否出现；filesystem-only 项是 `discoverable`，只有 Worker
advertisement/adapter-verified activation 才能证明 `callable`。状态含义如下：

- `discoverable`：HotPlex 找到有效文件定义，但当前 Worker 没有确认调用路径；filesystem-only Skill 属于此类，不能调用。
- `callable`：当前 Worker 的权威目录确认可原生执行，才允许调用。
- `unavailable`：能力表面明确报告不可用；同样不能调用。当前 filesystem-only 且未被 Worker 确认的 Skill 保持 `discoverable`。

短 `/name`（包括 WebChat）、显式 `/worker <name>`、busy replay 和 crash structured replay 共用当前 Session 的 callability 判定；不能用旧路径、缓存 metadata 或 `NativeInvoker` 绕过它。新建 Session 或 `/reset` 后再检查 `/skills`，因为配置和 Worker 目录证据按 Session 激活。

## 6. 修改与删除

- **修改真实 skill** 有两种方式：① 在线编辑——在 skill 详情的「Body」标签页直接改写
  `SKILL.md` 全文并保存（对应 `PUT /admin/api/skills/{name}`，仅真实 managed skill 可改）；
  ② 重新打包 zip 上传覆盖（勾选「覆盖同名」）。真实 global/project/user 项目按当前权限
  正常 update/delete。
- **内置 skill** 在 Admin/WebChat HTTP read surface 永久可发现，但不可直接 CRUD；builtin-only 对象 update/delete 返回
  `SKILL_BUILTIN_READONLY`。创建同名用户 override 会优先遮蔽内置项，并按真实 skill 正常管理。
- Worker projection 的 remove 与 Skills API 的删除是不同操作：前者只处理 receipt 证明归属
  且 tree 未改变的 native 文件，不删除 `$HOTPLEX_HOME` inventory。

## 7. REST API

除 WebChat UI 外，也可通过 REST API 管理（详见 [Admin API 参考](../reference/admin-api.md) 的「Skill 管理」章节）：

```bash
# 安装全局 skill（admin Bearer）
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -F "file=@my-skill.zip" "http://localhost:9999/admin/api/skills?replace=true"

# 在线更新全局 skill 的 SKILL.md 全文（admin Bearer）
curl -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d "{\"body\":\"$(jq -Rs . < SKILL.md)\"}" \
  "http://localhost:9999/admin/api/skills/my-skill"

# 安装 workspace skill（owner，Cookie 或 API Key）
curl -X POST -H "X-API-Key: $API_KEY" \
  -F "file=@my-skill.zip" "http://localhost:8888/api/workspaces/$WID/skills"

# 合并查询我的 skill 视图
curl -H "X-API-Key: $API_KEY" http://localhost:8888/api/skills
```
