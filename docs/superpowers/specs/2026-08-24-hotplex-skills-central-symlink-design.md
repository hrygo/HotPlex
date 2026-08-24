# HotPlex Skill 中心目录与 Agent 逐项软链接设计

## 背景

当前 Skill reconcile 会把同一内置包分别投影到不同 Worker 的原生目录：

- Codex/OpenCode：`~/.agents/skills/<name>`
- Claude Code：`~/.claude/skills/<name>`

这会产生重复文件、版本漂移和首次初始化门槛。当前主机上 `~/.claude/skills` 是真实目录，且 `hotplex-cli`、`hotplex-operator` 是由 HotPlex 管理的真实目录；对应中心目录位于 `~/.agents/skills`。

## 目标状态

`~/.agents/skills` 是 HotPlex Skill 的唯一实际同步目标。其他 Agent 的个性化目录只建立逐项软链接：

```text
~/.agents/skills/hotplex-cli       ← 实际内容
~/.claude/skills/hotplex-cli       → ~/.agents/skills/hotplex-cli
~/.agents/skills/hotplex-operator  ← 实际内容
~/.claude/skills/hotplex-operator  → ~/.agents/skills/hotplex-operator
```

如果 `~/.claude/skills` 本身已经解析为 `~/.agents/skills`，视为完整状态，不再创建或修改逐项链接。

## 行为约束

1. 同步中心目录仍使用现有事务、备份、receipt 和树哈希机制。
2. Claude 逐项链接只针对当前 profile 中的 HotPlex 包，不触碰其他用户 Skill。
3. 已经指向正确中心包的软链接返回 unchanged。
4. 缺失的包链接自动创建。
5. 同名真实目录、真实文件、坏链或指向其他位置的软链接返回 collision/drift，不自动覆盖。
6. 当前已有的旧 Claude receipt 必须能被迁移到中心 receipt/link 语义，避免迁移后 `status` 误报或 `remove` 失去所有权判断。
7. `remove` 只能删除 receipt 证明由 HotPlex 创建的逐项软链接和中心包；用户自建链接、真实目录和未知 Skill 不删除。
8. Codex/OpenCode 继续直接使用中心目录，两个 Worker 共享中心 receipt；Claude 的链接状态必须纳入 `status`、`sync`、`remove` 和 `dry-run` 报告。

## 设计

### 目标模型

扩展 reconcile target/receipt 模型，使一个中心投影可以带一个或多个 Agent alias roots。中心包的身份、树哈希和版本以 `~/.agents/skills/<name>` 为准；Claude 链接记录其 link path 和期望 target。receipt schema 升级保持向后兼容：旧的 Claude 真实目录 receipt 可以在首次 sync 时安全迁移为中心包 + link receipt，迁移前必须验证旧目录 receipt、树哈希和中心目录内容一致。

### 目录判定

逐项链接检查使用 `Lstat`，不跟随未知链接：

- 正确链接：`Readlink`/解析结果等于中心包，unchanged；
- 缺失：创建临时链接后原子 rename；
- 真实目录或文件：collision；
- 指向其他路径或坏链：collision；
- 根目录本身是中心目录软链接：整个 Agent root unchanged。

### 事务与失败

中心包内容先按现有流程提交。逐项链接创建失败时返回 failed/drift，保留中心包和 receipt，并在报告中给出具体链接路径；不删除或覆盖用户内容。链接删除也先移动到可恢复 tombstone/backup，再提交 receipt 变更。

### CLI 语义

保留现有 `--worker`、`--profile`、`--dry-run` 参数。选择 Claude 时，报告中心包和 Claude link 状态；选择 Codex/OpenCode 时只报告中心包；多个 Worker 同时选择时去重共享中心目标。`status` 明确显示 `linked`、`missing`、`collision` 或 `root_linked`。

## 测试范围

- 中心目录已存在、Claude 逐项正确链接：status unchanged。
- 中心目录存在、Claude 链接缺失：sync 创建链接。
- Claude 根目录整体链接到中心：不做逐项操作。
- Claude 同名真实目录/文件/错误链接：报告冲突且不覆盖。
- 旧 Claude receipt + 真实目录迁移为中心包 + 软链接。
- remove 只删除 HotPlex receipt 证明归属的链接和中心包。
- dry-run 不写入中心目录、链接或 receipt。
- Codex/OpenCode 共享中心包和 receipt，不重复复制。

## 非目标

- 不自动删除或移动用户已有非 HotPlex Skill。
- 不把整个 `~/.claude/skills` 目录替换成软链接。
- 不改变 `runtime`/`operator` profile 的权限边界。
- 不修改 WebChat/Agent 的 Skill 调用协议。
