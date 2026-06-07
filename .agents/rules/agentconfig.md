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
    <skills>    SKILLS.md
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
