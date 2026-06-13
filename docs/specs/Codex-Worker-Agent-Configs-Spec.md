---
type: spec
tags:
  - project/HotPlex
  - worker/codex
  - agent-config
  - system-prompt
date: 2026-06-11
status: implemented
progress: 100
out_of_scope: BaseInstructions/DeveloperInstructions injection — implemented via bridge.injectAgentConfig → worker.go buildThreadStartParams → JSON-RPC baseInstructions. DeveloperInstructions reserved for future use.
estimated_hours: 4
---

# Codex Worker Agent-Configs 注入规格

> **目标**：让 HotPlex 的 agent-configs（B/C 通道）体系在 Codex CLI Worker（`codex app-server` 模式）下正
> 常生效。当前 agent-configs 对 Codex Worker 完全不生效，`bridge.injectAgentConfig()` 写入的
> `session.SystemPrompt` 未通过 JSON-RPC 传递给 `codex app-server`。

---

## 1. 背景与现状

### 1.1 Agent-Configs 数据流（期望）

```
~/.hotplex/agent-configs/         B/C 通道文件
        ↓
agentconfig.Load() + BuildSystemPrompt()
        ↓
bridge.injectAgentConfig() → info.SystemPrompt = "合并后 prompt"
        ↓
Worker.Start(session SessionInfo) ← session.SystemPrompt
        ↓
buildThreadStartParams() → JSON-RPC "thread/start"
        ↓
codex app-server 开始使用 injected prompt
```

### 1.2 实际数据流（当前，断点 at ⚡）

```
agentconfig.Load() + BuildSystemPrompt()
        ↓
bridge.injectAgentConfig() → info.SystemPrompt = "合并后 prompt"  ← ✅ 正常工作
        ↓
AppServerWorker.Start(session SessionInfo) ⚡ SystemPrompt 未被消费
        ↓
buildThreadStartParams(session, cfg) ⚡ 没有使用 session.SystemPrompt
        ↓
thread/start JSON-RPC ⚡ 没有 baseInstructions / developerInstructions 字段
        ↓
codex app-server ← 完全不知道 agent-configs 的存在
```

### 1.3 协议层证据

**app-server 协议**（Rust 侧，`thread.rs:133-135`）：

```rust
pub struct ThreadStartParams {
    // ...
    pub base_instructions: Option<String>,       // ← 等价于 system prompt
    pub developer_instructions: Option<String>,   // ← 开发者级指令，更高优先级
    // ...
}
```

参数通过 `#[serde(rename_all = "camelCase")]` 序列化为 `baseInstructions` / `developerInstructions`。

`thread_processor.rs:1284-1285` 中的 `build_thread_config_overrides` 将这些值透传到 `ConfigOverrides`，
最终注入到 session 配置。协议层**完全支持**，但 HotPlex 从未使用。

**HotPlex codexcli 侧**（`types.go:148`）：

```go
type ThreadStartParams struct {
    Model          string `json:"model,omitempty"`
    CWD            string `json:"cwd,omitempty"`
    Sandbox        string `json:"sandbox,omitempty"`
    Personality    string `json:"personality,omitempty"`
    Ephemeral      bool   `json:"ephemeral,omitempty"`
    ApprovalPolicy string `json:"approvalPolicy,omitempty"`
    // ❌ 缺少 baseInstructions
    // ❌ 缺少 developerInstructions
}
```

### 1.4 `codex exec` 模式已废弃

HotPlex 曾通过 `codex exec` 模式（CLI 子进程）使用 Codex，但该模式已被废弃移除。
当前仅保留 `codex app-server` 模式（单例 HTTP+SSE JSON-RPC），本 spec 仅覆盖此模式。

---

## 2. 影响范围

| 组件 | 文件 | 变更类型 |
|------|------|----------|
| **ThreadStartParams** | `internal/worker/codexcli/types.go` | 增加字段 |
| **buildThreadStartParams** | `internal/worker/codexcli/worker.go` | 增加 SystemPrompt 传递 |
| **Start / startNewThread** | `internal/worker/codexcli/worker.go` | 确认 SystemPrompt 在 session 中 |
| **ResetContext** | `internal/worker/codexcli/worker.go` | 确认持久化 + 传递 |
| **UpdateSystemPrompt** | `internal/worker/codexcli/worker.go` | 验证（已实现） |
| **单元测试** | `internal/worker/codexcli/worker_test.go` | 新增 |

---

## 3. 方案设计

### 3.1 `ThreadStartParams` 增加字段

```go
// types.go
type ThreadStartParams struct {
    Model          string `json:"model,omitempty"`
    CWD            string `json:"cwd,omitempty"`
    Sandbox        string `json:"sandbox,omitempty"`
    Personality    string `json:"personality,omitempty"`
    Ephemeral      bool   `json:"ephemeral,omitempty"`
    ApprovalPolicy string `json:"approvalPolicy,omitempty"`

    // NEW: agent-configs 注入
    // baseInstructions 对应 HotPlex 的 session.SystemPrompt (B/C 通道合并后的 prompt)。
    // app-server 将其作为 base instructions (system prompt 基座)。
    BaseInstructions        string `json:"baseInstructions,omitempty"`
    // developerInstructions 优先级高于 baseInstructions。
    // HotPlex 暂不设置此字段，保留给未来扩展使用。
    DeveloperInstructions   string `json:"developerInstructions,omitempty"`
}
```

### 3.2 `buildThreadStartParams` 传递 SystemPrompt

```go
// worker.go - buildThreadStartParams()
func buildThreadStartParams(session worker.SessionInfo, cfg Config) map[string]any {
    params := map[string]any{
        "cwd":            session.ProjectDir,
        "sandbox":        sandboxFromSession(session, cfg.Sandbox),
        "personality":    cfg.Personality,
        "approvalPolicy": approvalMode,
    }
    // ...其他现有字段...

    // NEW: agent-configs 注入
    if session.SystemPrompt != "" {
        params["baseInstructions"] = session.SystemPrompt
    }
    // developerInstructions 暂不设置，保留给未来
    // if session.SystemPromptReplace != "" {
    //     params["developerInstructions"] = session.SystemPromptReplace
    // }

    return params
}
```

**关于 developerInstructions 的决策**：`developerInstructions`（对应 Rust 侧的 `ConfigOverrides`）
优先级高于 `baseInstructions`。当前 `SessionInfo.SystemPromptReplace` 在 Codex Worker 中没有对应概念
（`codex app-server` 没有等价的 runtime flag），
暂不映射。如果未来需要，可以通过 `developerInstructions` 实现更高优先级的指令覆盖。

### 3.3 `ResetContext` 确保 SystemPrompt 持久化

`ResetContext()` 中调用 `startNewThread(origSess, "reset")`，`origSess` 包含 `SystemPrompt`。
`UpdateSystemPrompt()` 已实现在 worker.go:562，bridge 在 reset 时调用它将最新的 agent config
更新到 `origSession.SystemPrompt`。所有 reset 路径已经正确。

### 3.4 注入与传递时序验证

```
Session 初始化:
  bridge.createAndLaunchWorker()
    → injectAgentConfig()          ← session.SystemPrompt 被写入
    → Start(session)               ← SystemPrompt 在 SessionInfo 中
      → startNewThread(session, "start") ← SystemPrompt 传入 buildThreadStartParams
        → thread/start { baseInstructions: "..." } ← app-server 接收

Session Reset:
  bridge.resetWorker()
    → worker.ResetContext()
      → UpdateSystemPrompt()       ← bridge 在 ResetContext 后调用
      → startNewThread(origSess, "reset") ← origSess.SystemPrompt 已更新

Session Resume:
  callback code executes Start(session) again
    → 走 Start() + startNewThread() 路径，SystemPrompt 正常传递
```

---

## 4. 验收条件 (Acceptance Criteria)

### AC-01: 新建会话时 agent-configs 生效

**Given** 用户配置了 `~/.hotplex/agent-configs/SOUL.md`（任意 B/C 通道文件）
**When** 通过飞书/Slack WebChat 发起一个新会话，worker 类型为 `codex_cli`
**Then** 以下条件全部满足：
- [ ] `bridge.injectAgentConfig()` 的日志输出 `"bridge: agent config injected"`，`prompt_len > 0`
- [ ] `buildThreadStartParams()` 生成的 params 包含 `"baseInstructions"` 键
- [ ] `baseInstructions` 的值包含对应 B/C 通道文件内容（如 SOUL.md 的内容）
- [ ] Codex 代码助理在回复中体现 SOUL.md 中的身份设定

**证据链**
- `bridge_worker.go:82` → `b.injectAgentConfig(&params.workerInfo, ...)`
- `bridge_worker.go:367` → `info.SystemPrompt = prompt`
- `worker.go:324` → `params := buildThreadStartParams(session, cfg)`
- `worker.go:667+` → 新逻辑: `if session.SystemPrompt != "" { params["baseInstructions"] = ... }`
- Rust `thread.rs:133` → `pub base_instructions: Option<String>`

### AC-02: 系统重置 (/reset) 后 agent-configs 重新生效

**Given** 一个会话正在运行中，`agent-configs` 文件被用户修改
**When** 用户执行 `/reset` 命令
**Then** 以下条件全部满足：
- [ ] Bridge 在 `resetWorker()` 中调用 `injectAgentConfig` 并触发 `UpdateSystemPrompt()`
- [ ] `worker.go:562` 的 `UpdateSystemPrompt()` 更新 `origSession.SystemPrompt`
- [ ] `ResetContext()` 调用 `startNewThread(origSess, "reset")`
- [ ] `origSess.SystemPrompt` 包含最新加载的 agent config
- [ ] 新 thread 的 `baseInstructions` 反映最新的 agent config 内容

**证据链**
- `bridge.go:489` → `b.injectAgentConfig(info, ...)`
- `bridge.go:491` → `su.UpdateSystemPrompt(info.SystemPrompt)`
- `worker.go:538` → `w.startNewThread(origSess, "reset")`

### AC-03: SystemPrompt 为空时不影响正常行为

**Given** 没有配置任何 agent-configs（目录不存在或全空）
**When** 发起一个新会话
**Then** 以下条件全部满足：
- [ ] `injectAgentConfig()` 日志输出 `"bridge: agent config empty"` 或 `"agent config dir disabled"`
- [ ] `session.SystemPrompt` 保持 `""`
- [ ] `buildThreadStartParams()` 不设置 `baseInstructions` 字段
- [ ] Codex 正常工作，无异常

### AC-04: DeveloperInstructions 字段保留（不膨胀）

**Given** 代码变更后
**When** 审查 `ThreadStartParams` 结构体
**Then** 以下条件全部满足：
- [ ] `DeveloperInstructions` 字段已声明但不在 `buildThreadStartParams()` 中设置
- [ ] 注释明确说明 future use only
- [ ] `buildThreadStartParams()` 不设置 `"developerInstructions"` key

### AC-05: 向后兼容

**Given** 现有 codex app-server 版本（不支持 `baseInstructions` 的版本）
**When** HotPlex 发送 `baseInstructions` 字段
**Then** 以下条件全部满足：
- [ ] `baseInstructions` 字段使用 `omitempty`，空 string 时不发送
- [ ] 旧版 app-server 忽略未知字段（JSON-RPC 特性，server 端反序列化容忍额外字段）
- [ ] 没有 panic、crash 或 connection 断裂

### AC-06: Resume 后 agent-configs 仍然生效

**Given** 用户连续两次发起会话到同一目录（触发 resume），agent-configs 已配置
**When** 第二个会话启动
**Then** 以下条件全部满足：
- [ ] `Resume()` 方法调用中 `session.SystemPrompt` 非空
- [ ] `buildThreadStartParams()` 的 JSON-RPC 调用包含 `baseInstructions`
- [ ] 恢复后的会话仍然遵循 agent-configs 中的身份设定

### AC-07: inject_exclude 过滤仍然生效

**Given** agent-configs 目录包含 `SOUL.md` 和 `MEMORY.md`，且 `inject_exclude` 配置排除了 `MEMORY.md`
**When** 发起一个会话
**Then** 以下条件全部满足：
- [ ] `BuildSystemPrompt()` 返回的 prompt 仅包含 `SOUL.md`，不包含 `MEMORY.md`
- [ ] `baseInstructions` 的值等于排除 `MEMORY.md` 后的合并 prompt
- [ ] Codex 助理看到 SOUL.md 的指令但不看到 MEMORY.md 的内容

**证据链**
- `bridge_worker.go:305` → `resolveInjectExclude()`
- `bridge_worker.go:329` → `injectAgentConfig()` 接受 `injectExclude` 参数

### AC-08: 多 Bot 场景下路径隔离正确

**Given** 配置了两个 Feishu Bot（Bot-A 和 Bot-B），各自有独立的 agent-configs 目录
**When** 分别通过 Bot-A 和 Bot-B 发起会话
**Then** 以下条件全部满足：
- [ ] Bot-A 的会话中 `baseInstructions` 包含 Bot-A 专属的 agent config
- [ ] Bot-B 的会话中 `baseInstructions` 包含 Bot-B 专属的 agent config
- [ ] 两个 Bot 的 agent config 互不干扰

### AC-09: 单测覆盖

**Given** 代码变更后
**When** 运行 `make test` 或 `go test ./internal/worker/codexcli/...`
**Then** 以下条件全部满足：
- [ ] `TestBuildThreadStartParams` 覆盖 `SystemPrompt` 非空场景
- [ ] `TestBuildThreadStartParams` 覆盖 `SystemPrompt` 为空场景
- [ ] `TestBuildThreadStartParams` 覆盖 `DeveloperInstructions` 始终不存在的场景
- [ ] 所有测试通过，无 regression

### AC-10: 其他 Worker 零影响

**Given** 本 spec 仅修改 `codexcli/` 包
**When** 部署变更
**Then** 以下条件全部满足：
- [ ] `internal/worker/claudecode/`、`internal/worker/acp/`、`internal/worker/opencodeserver/` 零改动
- [ ] 非 Codex Worker 的 behavior 完全不变

---

## 5. 实现计划

### Step 1: `types.go` - ThreadStartParams 新增字段

```go
// 在现有字段后追加
BaseInstructions        string `json:"baseInstructions,omitempty"`
DeveloperInstructions   string `json:"developerInstructions,omitempty"`
```

### Step 2: `worker.go` - buildThreadStartParams 传递 SystemPrompt

```go
// 在 return params 前追加
if session.SystemPrompt != "" {
    params["baseInstructions"] = session.SystemPrompt
}
```

### Step 3: `worker.go` - Start / startNewThread / ResetContext 验证

验证 `startNewThread(session, ...)` 接收的 `session.SystemPrompt` 不为空时正确传递。
当前代码路径已验证：`startNewThread` → `buildThreadStartParams`，无需额外改造。

### Step 4: 单元测试

- `TestBuildThreadStartParams` 扩展：
  - "with system prompt" case
  - "without system prompt" case
  - "system prompt not set as developerInstructions" case

### Step 5: 验证

- 本地运行 `make test` 确认无 regression
- 运行 `go test -count=1 -race ./internal/worker/codexcli/... -v` 确认测试通过

---

## 6. 不在此范围

| 项目 | 理由 |
|------|------|
| `codex exec` 模式 | 已废弃移除，不在本 spec 范围内 |
| `developerInstructions` 实际使用 | 当前 `SessionInfo` 没有对应概念，保留字段供未来扩展 |
| `nReplace` / `SystemPromptReplace` 映射 | Codex app-server 协议层没有明确的 "replace" 语义，现有 `baseInstructions` 是追加语义 |
| `codex` 二进制零修改 | HotPlex 侧的单向修改即可，app-server 协议已经全量支持额外字段容忍 |
| ACP / OpenCodeServer / Claude Code Worker | 这些 Worker 的 agent-configs 已经通过各自的 `UpdateSystemPrompt()` + 运行时注入正常工作了 |

---

## 7. 回滚方案

如果部署后发现 regressions：

1. 回退 `types.go` 和 `worker.go` 的修改
2. `UpdateSystemPrompt()` 无需回退（已经是安全的 no-op 级别的存储）
3. `bridge.go` / `bridge_worker.go` 零改动，无需回退
