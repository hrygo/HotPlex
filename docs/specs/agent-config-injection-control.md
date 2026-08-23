# Spec: Agent Config Injection Control

> **Implemented** — the `inject_exclude` feature described below was implemented in PR#603.
> This document now serves as the design reference.

| 字段     | 值                                          |
|----------|---------------------------------------------|
| 状态     | Implemented                                  |
| 日期     | 2026-05-31                                  |
| 作者     | 黄飞虹                                      |
| 影响模块 | `internal/agentconfig`, `internal/config`, `internal/gateway`, `cmd/hotplex` |

---

## 1. 动机

宿主 Agent 框架（如 Hermes）自带 SOUL/AGENTS/Skills/Memory 体系，与 HotPlex agent-configs 的 B/C 通道全量注入产生冲突。当宿主已经管理了 persona 或 memory 时，HotPlex 再次注入同名文件会导致指令冲突或内容覆盖。

当前唯一控制手段是 `agent_config.enabled: false`（杀死所有注入），粒度太粗。需要 **per-file 粒度**的注入控制。

**核心场景**：不同 bot 使用不同 worker（甚至同为 ACP 但不同 agent），注入需求不同：

```
Bot A (飞书, acp + hermes)    → 跳过 SOUL.md, MEMORY.md (Hermes 自带)
Bot B (飞书, acp + other)     → 全量注入 (other-agent 无冲突)
Bot C (飞书, claudecode)      → 全量注入
Bot D (Slack, claudecode)     → 跳过 AGENTS.md (特殊需求)
Webchat / API session         → 用全局默认
```

## 2. 现状

```
config.yaml:
  agent_config:
    enabled: true           # 总开关，false = 完全跳过
    config_dir: ~/.hotplex/agent-configs
```

- `Load(dir, platform, botID)` 加载 5 个文件（SOUL/AGENTS/TOOLS/USER/MEMORY），全量组装，无法按文件跳过
- `META-COGNITION.md` 始终注入（`go:embed`），不可配置
- 唯一开关是 `enabled: false`

### 调用点（4 处）

| 文件 | 行号 | 用途 |
|------|------|------|
| `internal/gateway/bridge_worker.go` | :254 | Worker session 注入 system prompt |
| `cmd/hotplex/bot_config_adapter.go` | :93 | Admin API：获取 bot 配置详情 |
| `cmd/hotplex/bot_config_adapter.go` | :116 | Admin API：获取 system prompt 文本 |
| `cmd/hotplex/bot_config_adapter.go` | :391 | Admin API：agent config summary |

## 3. 目标

1. 每个 agent-config 文件可独立控制是否注入（inject / skip）
2. 遵循现有三级配置 fallback：**global → platform → per-bot**，零配置 = 当前全量注入（向后兼容）
3. 同平台单 bot 配置（`feishu.inject_exclude`）和多 bot 配置（`feishu.bots[].inject_exclude`）均受支持
4. `META-COGNITION.md` 不受影响，始终注入
5. 所有 4 个调用点行为一致
6. 支持配置热重载

## 4. 配置模型

### 4.1 YAML 示例

```yaml
# ① 全局默认 — webchat/API session（无 botID）使用
agent_config:
  enabled: true
  config_dir: ~/.hotplex/agent-configs
  inject_exclude: [AGENTS.md]                   # 可选，全局默认

messaging:
  feishu:
    # ② 平台级默认 — 该平台所有 bot 的 fallback（单 bot 配置也在此）
    inject_exclude: [SOUL.md]

    # ③ Per-bot 覆盖 — 最高优先级
    bots:
      - app_id: "ou_hermes_xxx"
        worker_type: acp
        acp_command: "hermes"
        inject_exclude: [SOUL.md, MEMORY.md]     # Hermes 自带 persona/memory

      - app_id: "ou_normal_yyy"
        worker_type: acp
        acp_command: "other-agent"
        # 无 inject_exclude → fallback 到平台级 [SOUL.md]

      - app_id: "ou_cc_zzz"
        worker_type: claudecode
        inject_exclude: []                        # 显式清空，覆盖平台级

  slack:
    inject_exclude: []                            # Slack 平台不跳过
    bots:
      - bot_token: "xoxb-..."
        inject_exclude: [AGENTS.md]               # 仅此 bot 跳过
```

### 4.2 优先级链

```
bot.inject_exclude          → 优先使用（per-bot 覆盖）
  ↓ nil / 未配置
platform.inject_exclude     → 平台默认
  ↓ nil / 未配置
agent_config.inject_exclude → 全局默认
  ↓ nil / 未配置
nil                     → 全量注入（向后兼容）
```

### 4.3 Go 结构体

```go
// internal/config/config.go

// ① 全局层
type AgentConfig struct {
    Enabled   bool     `mapstructure:"enabled"`
    ConfigDir string   `mapstructure:"config_dir"`
    InjectExclude []string `mapstructure:"inject_exclude"` // NEW: 全局默认
}

// ② 平台层（嵌入 SlackConfig / FeishuConfig）
type MessagingPlatformConfig struct {
    WorkerType string `mapstructure:"worker_type"`
    InjectExclude  []string `mapstructure:"inject_exclude"` // NEW: 平台级默认
    STTConfig  `mapstructure:",squash"`
    TTSConfig  `mapstructure:",squash"`
}

// ③ Per-bot 层
type SlackBotConfig struct {
    // ... existing fields ...
    InjectExclude []string `mapstructure:"inject_exclude,omitempty"` // NEW: per-bot 覆盖
}

type FeishuBotConfig struct {
    // ... existing fields ...
    InjectExclude []string `mapstructure:"inject_exclude,omitempty"` // NEW: per-bot 覆盖
}
```

### 4.4 设计决策

- **黑名单而非白名单**——默认全量注入（零配置向后兼容），只声明要跳过的文件
- 列表值为**文件基名**（`SOUL.md`），不含路径，大小写不敏感匹配
- **跨三级 agent-config fallback 生效**：`inject_exclude: [SOUL.md]` 会跳过该文件在所有三级目录（global / platform / bot）中的查找。`shouldExclude` 判断在 `resolveFile` 之前，命中的文件无论存在于哪一级都不会被加载
- `META-COGNITION.md` 出现在任何列表中 → Warn 日志 + 忽略（不可跳过）
- 遵循现有 `FillFrom` / `propagateBotDefaults` 传播模式

## 5. 代码改动

### 5.1 `internal/agentconfig/loader.go`

**签名变更**：

```go
// Before
func Load(dir, platform, botID string) (*AgentConfigs, error)

// After
func Load(dir, platform, botID string, injectExclude []string) (*AgentConfigs, error)
```

**新增内部函数**：

```go
func shouldExclude(name string, injectExclude []string) bool {
    for _, s := range injectExclude {
        if strings.EqualFold(name, s) {
            return true
        }
    }
    return false
}
```

**改动位置**：`load` 闭包内加前置判断（当前第 53 行起）：

```go
load := func(baseName string, target *string) error {
    if shouldExclude(baseName, injectExclude) {
        return nil
    }
    // ... 原有逻辑不变
}
```

### 5.2 `internal/agentconfig/prompt.go`

**无需改动**。被跳过的文件对应字段为空字符串，`BuildSystemPrompt` 已正确跳过空字段。

### 5.3 `internal/config/config.go`

**三处结构体新增字段**（见 4.3）

**传播逻辑扩展**：

```go
// propagateBotDefaults 扩展 — 传播 inject_exclude 从平台到 bot
func propagateBotDefaults(platformCfg *MessagingPlatformConfig, botSTT *STTConfig, botTTS *TTSConfig, botSkip *[]string) {
    botSTT.FillFrom(platformCfg.STTConfig)
    botTTS.FillFrom(platformCfg.TTSConfig)
    if *botSkip == nil {
        *botSkip = platformCfg.InjectExclude // nil = 未配置 → 继承平台级
    }
}
```

**normalize 函数扩展**：单 bot 配置包装时传播 `InjectExclude`：

```go
// normalizeSlackBots — 自动包装时保留平台级 InjectExclude
cfg.Bots = []SlackBotConfig{
    {Name: "default", BotToken: cfg.BotToken, AppToken: cfg.AppToken, InjectExclude: cfg.InjectExclude},
}
```

### 5.4 `internal/gateway/deps.go` + `internal/gateway/bridge.go`

**BridgeDeps 新增字段**：

```go
type BridgeDeps struct {
    // ... existing fields ...
    AgentConfigInjectExclude atomic.Value  // NEW: map[string][]string (botID → injectExclude)
}
```

**Bridge 新增字段 + 方法**：

```go
type Bridge struct {
    // ... existing fields ...
    agentConfigInjectExclude atomic.Value  // map[string][]string; "" key = 全局默认
}

// UpdateAgentConfigSkip replaces the skip-files map (called on config hot-reload).
func (b *Bridge) UpdateAgentConfigSkip(m map[string][]string) {
    b.agentConfigInjectExclude.Store(m)
}
```

**调用点改动**（`bridge_worker.go:254`）：

```go
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botID string) {
    if b.agentConfigDir == "" {
        return
    }
    // 解析 inject_exclude: botID 查询 → "" fallback → nil (全量注入)
    injectExclude, _ := b.agentConfigInjectExclude.Load().(map[string][]string)
    var skip []string
    if botID != "" {
        skip = injectExclude[botID]
    }
    if skip == nil {
        skip = injectExclude[""] // 全局默认
    }
    configs, err := agentconfig.Load(b.agentConfigDir, platform, botID, skip)
    // ...
}
```

### 5.5 `cmd/hotplex/gateway_run.go`

**初始化构建 skip map**：

```go
func buildAgentConfigSkipMap(cfg *config.Config) map[string][]string {
    m := make(map[string][]string)
    // 全局默认
    if len(cfg.AgentConfig.InjectExclude) > 0 {
        m[""] = cfg.AgentConfig.InjectExclude
    }
    // Slack bots
    for _, bot := range cfg.Messaging.Slack.Bots {
        if bot.InjectExclude != nil {
            m[bot.BotToken] = bot.InjectExclude // BotToken 作为 botID
        }
    }
    // Feishu bots
    for _, bot := range cfg.Messaging.Feishu.Bots {
        if bot.InjectExclude != nil {
            m[bot.AppID] = bot.InjectExclude // AppID 作为 botID
        }
    }
    return m
}
```

**Bridge 初始化 + 热重载**：

```go
// 初始化
bridge := gateway.NewBridge(gateway.BridgeDeps{
    // ...
    AgentConfigInjectExclude: buildAgentConfigSkipMap(cfg),
})

// 热重载（复用 ConfigStore.RegisterFunc 模式）
cfgStore.RegisterFunc(func(prev, next *config.Config) {
    oldSkip := buildAgentConfigSkipMap(prev)
    newSkip := buildAgentConfigSkipMap(next)
    if !reflect.DeepEqual(oldSkip, newSkip) {
        bridge.UpdateAgentConfigSkip(newSkip)
        log.Info("config: agent config skip files updated")
    }
})
```

### 5.6 `cmd/hotplex/bot_config_adapter.go`

Adapter 需要按 botID 查询 skip files：

```go
// 3 处 Load 调用（:93, :116, :391）统一变更
// 从 config store 按平台+botID 查找 bot 配置，提取 InjectExclude
injectExclude := resolveInjectExclude(a.cfgStore, platform, botID)
configs, err := agentconfig.Load(a.agentConfigDir, platform, botID, injectExclude)
```

## 6. 测试

### 6.1 单元测试 `internal/agentconfig/loader_test.go`

| 用例 | 说明 |
|------|------|
| `TestLoad_NoSkip` | `injectExclude=nil`，行为与当前完全一致 |
| `TestLoad_SkipSingle` | `injectExclude=["SOUL.md"]`，Soul 为空，其余正常 |
| `TestLoad_SkipMultiple` | 跳过多个文件 |
| `TestLoad_SkipAll` | 跳过所有 5 个文件，`IsEmpty() == true` |
| `TestLoad_SkipCaseInsensitive` | `injectExclude=["soul.md"]` 匹配 `SOUL.md` |
| `TestLoad_SkipMetaCognition` | `injectExclude=["META-COGNITION.md"]`，Warn 日志 + 忽略 |

### 6.2 配置传播测试 `internal/config/config_test.go`

| 用例 | 说明 |
|------|------|
| `TestPropagateBotDefaults_InjectExclude` | bot nil → 继承平台级 |
| `TestPropagateBotDefaults_InjectExcludeOverride` | bot 非 nil → 使用 bot 自己的 |
| `TestNormalizeSlackBots_InjectExclude` | 单 bot 包装时 InjectExclude 传播 |

### 6.3 集成验证

- `config.yaml` 不含 `inject_exclude` → 全量注入（向后兼容）
- `feishu.bots[].inject_exclude: [SOUL.md]` → 该 bot 无 `<persona>` 段
- `feishu.inject_exclude: [SOUL.md]` + bot 无 inject_exclude → 该平台所有 bot 继承
- 热重载 `inject_exclude` → 下一个 session 生效

## 7. 配置默认值与向后兼容

| 配置状态 | 行为 |
|----------|------|
| 无任何 `inject_exclude` 键 | 全量注入（与当前完全一致） |
| 仅 `agent_config.inject_exclude: [SOUL.md]` | 全局默认跳 SOUL，所有 session 生效 |
| 仅 `feishu.inject_exclude: [SOUL.md]` | 飞书 bot 跳 SOUL，webchat/Slack 不受影响 |
| `feishu.bots[].inject_exclude: []` | 显式清空，覆盖平台级 |
| `enabled: false` | 完全跳过（`Load` 不被调用，`inject_exclude` 无效） |

## 8. 不在范围内

- Per-level skip（如「仅跳过 global 级 SOUL.md，保留 bot 级覆盖」）——当前跨三级跳过已满足宿主冲突场景
- `META-COGNITION.md` 可配置化（设计上始终注入）
- Admin API 读写 `inject_exclude`（可后续扩展到 bot 配置管理接口）
