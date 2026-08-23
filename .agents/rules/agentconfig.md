---
paths:
  - "**/agentconfig/**/*.go"
---

# Agent Config

`Load(dir, platform, botName, injectExclude...)` 按三级 fallback 加载配置：`{botName}/` → `{platform}/` → 全局，每文件独立，命中即终止。`botName=""` 时跳过 bot 级查找。`injectExclude` 为黑名单（大小写不敏感），命中的文件跳过加载。

**B/C 双通道**：冲突时 directives 无条件覆盖 context。

```xml
<agent-configuration>
  <directives>
    <hotplex>   META-COGNITION.md (go:embed, 首位, 始终存在)
    <persona>   SOUL.md
    <rules>     AGENTS.md
    <tool-guidance> TOOLS.md
  </directives>
  <context>
    <user>      USER.md
    <memory>    MEMORY.md
  </context>
</agent-configuration>
```

- 注入：CC → `--append-system-prompt` | OCS → `system` 字段
- BotName：`Adapter.botName(YAML 配置名) → Bridge → injectAgentConfig → Load`
- 限制：单文件 8KB / 总量 40KB | YAML frontmatter 自动剥离
- 安全：`ValidateBotName(botName)` 防路径穿越（含 `"."` / `".."` 检查）
- 注入排除：`injectExclude` 按文件名（大小写不敏感）跳过加载；3 级配置（bot → platform → global），nil 继承，空 slice 覆盖清空

**双轨（spec ②）**：WebChat 多租户轨与 Message Channel 轨隔离解析，互不污染。

- **Message Channel 轨**（Slack/Feishu）：`Load` 按 Bot → 平台 → 全局逐文件解析，缺失才继承，present-empty 显式清空并终止。
- **WebChat 轨**：`LoadForWorkspace(dir, platform, overrides, injectExclude...)` 两层继承——团队默认（`Load` base）→ workspace overrides 逐文件覆盖（命中即终止，空值显式清空，`injectExclude` 优先级最高）。
- **workspace overrides**：DB JSON flat map（`workspaces.agent_config_overrides` 列，复用 spec ① 无新迁移），`ValidateOverrides` 校验键白名单/类型/size（PATCH 写入侧 + Bridge 读取侧复用同一函数）。
- **分流判据**：`injectAgentConfig` 以 `workspaceOverrides != nil` 区分双轨；`Bridge.resolveWorkspaceOverrides(workspaceID)` 按 `workspace_id` 解析（空 → nil → Message Channel 轨）。3 个 worker 启动调用点（StartSession / resume / fresh-start）+ ResetSession 均经此 helper。
- **不可覆盖**：`META-COGNITION.md` 不在 5 文件白名单（SOUL/AGENTS/TOOLS/USER/MEMORY），PATCH 拒、`applyOverrides` 忽略，物理不可覆盖。
