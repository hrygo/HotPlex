---
title: Skill 配置教程
weight: 7
description: 用 zip 上传管理 AI 技能 —— HotPlex skill 管理与各 Worker 软链引导
---

# Skill 配置教程

Skill 是封装了特定能力的 AI 技能包（含 `SKILL.md` 定义 + 辅助文件）。HotPlex 支持通过 WebChat UI 上传 zip 包安装 skill，统一存储在 `.agents/skills` 目录（对齐开放 `.agents` 标准）。完整规格见 [Skill-Management-Spec](../specs/Skill-Management-Spec.md)。

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

安装成功后，skill 立即出现在列表：`.agents/skills` 下的标注「可管理」，`.claude`/`.hotplex` 等目录下的标注「只读外部」。

## 4. 各 Worker 加载与软链引导

> ⚠️ **关键**：HotPlex **不向任何 Worker 传递 skill 目录参数**——各 Worker 按自身规则读取 home 下的 skill 目录。统一存储在 `~/.agents/skills`，但不同 Worker 的原生扫描路径不同，**Claude Code 需要额外建软链**。

| Worker | 原生读 `~/.agents/skills` | 需要的操作 |
|--------|--------------------------|-----------|
| **Codex CLI** | ✅ 主路径就是 `.agents/skills` | 无需任何操作 |
| **OpenCode Server** | ✅ agent-compat 扫描 `.agents/skills` | 无需任何操作 |
| **Claude Code** | ❌ 只读 `.claude/skills` | **必须建立软链**（见下） |
| **ACP** | 取决于底层 agent | 按底层 agent 处理 |

### Claude Code 软链设置（CC 专属）

Claude Code 只读 `~/.claude/skills`，需把统一存储目录软链过去：

```bash
# 若 ~/.claude/skills 已是真实目录（非软链），请先备份/迁移其内容，再执行：
ln -s ~/.agents/skills ~/.claude/skills
```

验证：

```bash
ls -l ~/.claude/skills
# 应显示: ~/.claude/skills -> ~/.agents/skills 的符号链接
```

建立软链后，新会话即可加载 `~/.agents/skills` 下的全部 skill。

> ⚠️ **为什么不由程序代建软链？** 软链涉及「已有真实目录被覆盖、方向冲突、跨 Worker 归一化」等数据安全风险。HotPlex 把这一步留给用户显式完成，避免程序误覆盖用户已有的 `.claude/skills` 内容——这是有意的设计决策（spec §2「软链管理」）。

## 5. 修改与删除

- **修改 skill** 有两种方式：① 在线编辑——在 skill 详情的「Body」标签页直接改写 `SKILL.md` 全文并保存（对应 `PUT /admin/api/skills/{name}`，仅 `managed` skill 可改）；② 重新打包 zip 上传覆盖（勾选「覆盖同名」）。两种方式都只更新 `SKILL.md`，包内其他文件需通过 zip 覆盖替换。
- **删除 skill** = 列表中点击删除。仅 `managed` skill（`.agents/skills` 下）可删；`.claude`/`.hotplex` 下的只读外部 skill 需在文件系统手动删除。

## 6. REST API

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
