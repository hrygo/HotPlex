# WebChat 多租户 spec ②：per-workspace agent-configs 自定义（两层继承）

**日期**: 2026-06-16
**状态**: Draft（待实现计划）
**分支**: main · **基线**: spec ① 已合入（[PR #746](https://github.com/hrygo/hotplex/pull/746)，`44f461ff`）
**范围**: WebChat 轨 agent-configs 从全局/Bot 级共享升级为 **per-workspace 两层继承**（团队默认 → workspace 自定义）
**关联设计**: [`WebChat-Multitenancy-Foundation-Design-Spec.md`](./WebChat-Multitenancy-Foundation-Design-Spec.md)（spec ①）、[`WebChat-Multitenancy-Roadmap-Spec.md`](./WebChat-Multitenancy-Roadmap-Spec.md)（路线图 §4 spec ②）
**作者**: brainstorming session → 设计文档

---

## 目录

- [1. 背景与现状](#1-背景与现状)
- [2. 目标与非目标](#2-目标与非目标)
- [3. 关键决策汇总](#3-关键决策汇总)
- [4. 数据模型与 JSON Schema](#4-数据模型与-json-schema)
- [5. 两层继承解析（LoadForWorkspace）](#5-两层继承解析loadforworkspace)
- [6. 双轨隔离](#6-双轨隔离)
- [7. 数据流打通](#7-数据流打通)
- [8. PATCH 校验与 API](#8-patch-校验与-api)
- [9. META-COGNITION 约束与安全](#9-meta-cognition-约束与安全)
- [10. 错误码](#10-错误码)
- [11. 测试策略](#11-测试策略)
- [12. 受影响文件清单](#12-受影响文件清单)
- [13. 后续 spec 路线](#13-后续-spec-路线)

---

## 1. 背景与现状

spec ①（地基）已交付真实用户身份、workspace 实体、会话隔离与多租户配额框架，并为 spec ② 预留了关键扩展点：

| 预留点 | 现状 | 证据 |
|---|---|---|
| `workspaces.agent_config_overrides` 列 | 已建（TEXT，spec ① 留 NULL） | `internal/session/sql/migrations/017_multitenancy_tables.sql:23` |
| `Workspace.AgentConfigOverrides` Go 字段 | 已声明，原样存取 | `internal/session/multitenancy_store.go:18`（`GetWorkspaceByID` :211 / `UpdateWorkspace` :244） |
| workspace PATCH 接受该字段 | 已通，**无任何校验** | `internal/gateway/workspace_handlers.go:130,162-163` |

**当前 agent-configs 注入链（spec ① 未改动，仍是 Message Channel 轨的三级 fallback）**：

```
workerLaunchParams{platform, botName, injectExclude}          // bridge_worker.go:31-40（无 workspace 维度）
  → createAndLaunchWorker (bridge_worker.go:82)
    → injectAgentConfig(info, platform, botName, botID, exclude)  // bridge_worker.go:328-364
      → agentconfig.Load(dir, platform, botName, exclude...)      // loader.go:53
        → resolveFile: bot → platform → global（每文件命中即终止） // loader.go:177-216
      → BuildSystemPrompt(configs) → info.SystemPrompt            // prompt.go:24
```

WebChat 会话当前 `botName=""`（`bridge_worker.go:333` 注释），走 `platform="webchat"` → global。**`workerLaunchParams` 完全无 workspace 概念**——这是 spec ② 要打通的核心。

**核心结论**：agent-configs 解析目前是单一目录三级 fallback，无 per-workspace 维度。spec ② 在不动 Message Channel 轨的前提下，为 WebChat 轨新增独立的 workspace 级配置继承路径。

---

## 2. 目标与非目标

### 2.1 目标（本 spec 范围）

1. **workspace 级 agent-configs 定制**：WebChat 轨每个 workspace 拥有独立的 SOUL/AGENTS/SKILLS/USER/MEMORY 覆盖，覆盖团队默认。
2. **两层继承**：团队默认（全局目录）→ workspace 自定义（DB JSON），逐文件覆盖。
3. **双轨隔离**：Message Channel 轨（Slack/Feishu）的三级 fallback 链零改动。
4. **META-COGNITION 不可覆盖**：Worker 身份边界由 go:embed 强制注入，workspace 无法触及。
5. **校验闭环**：workspace PATCH 对 `agent_config_overrides` 做 JSON/键白名单/size 三层校验。

### 2.2 非目标（明确排除）

| 项 | 归属 |
|---|---|
| WebChat 前端配置编辑 UI | spec ⑥ |
| admin 锁定某些文件禁止 workspace 覆盖 | YAGNI，未来增量（§3 决策 5） |
| per-user 层（用户级配置） | 永不（spec ① §2.4 拍板两层，无 per-user） |
| Message Channel 引入 workspace 配置 | 永不（双轨完全隔离） |
| workspace 级 worker 选择 | spec ③（独立 spec） |
| 配置版本控制 / 变更审计 | YAGNI |

---

## 3. 关键决策汇总

| 决策点 | 选择 | 理由 |
|---|---|---|
| 存储形式 | **DB JSON**（`agent_config_overrides` 列，spec ① 已建） | WebChat 用户经浏览器无法写服务器文件系统；spec ⑥ 前端编辑 UI 必须走 API→DB；per-workspace 隔离由 `owner_user_id` 天然保证；备份/迁移随 DB |
| 继承语义 | **逐文件覆盖**（命中即终止） | 与现有 `resolveFile` 语义完全一致；可预测；不叠加突破 40KB 总量上限 |
| 可覆盖范围 | **全部 5 文件**（SOUL/AGENTS/SKILLS/USER/MEMORY） | 与团队默认文件集合一致；受信用户（admin 邀请制）对自己 workspace 负责；底线由 META-COGNITION 保证 |
| 解析入口 | **新增 `LoadForWorkspace` 独立函数** | 双轨物理隔离最彻底；`Load` 零改动向后兼容；符合 spec ① §5「接口扩展而非替换」 |
| JSON 结构 | **flat map**（文件名 → 内容） | 键即文件名，与 `resolveFile`/`injectExclude` 按文件名模型零映射成本 |
| admin 锁定机制 | **不做**（YAGNI） | 第一版全开放；未来如需「锁定 AGENTS.md」可增量加 `locked_files` 字段 |
| 数据流载体 | `workerLaunchParams` 携带已解析 `workspaceOverrides map` | 保持 `injectAgentConfig` 纯函数（不查 store）；解析集中在 bridge helper（Bridge 加窄接口 `WSStore` 依赖，因 resume/fresh-start 在 bridge 内部触发） |

---

## 4. 数据模型与 JSON Schema

### 4.1 列与结构

复用 spec ① 已建列，**无新迁移**：

- `workspaces.agent_config_overrides TEXT`（nullable，JSON 字符串）
- `Workspace.AgentConfigOverrides string`（Go，原样存取，`multitenancy_store.go:18`）

### 4.2 JSON Schema：flat map

```json
{
  "SOUL.md":   "你是一个资深 Go 工程师，重视简洁与可读性...",
  "AGENTS.md": "本项目使用 tab 缩进；测试用 testify/require...",
  "SKILLS.md": "可调用 /commit、/review 等技能...",
  "USER.md":   "用户偏好简洁回复，中文沟通",
  "MEMORY.md": "上周讨论了 session key 改造方案..."
}
```

- **键**：文件名，大小写敏感，限定白名单 `{"SOUL.md","AGENTS.md","SKILLS.md","USER.md","MEMORY.md"}`（即 `loader.go:121` 的 `configFiles`）。
- **值**：文件内容字符串。不经过 `stripFrontmatter`（DB 是用户/API 直接编辑的内容，非文件场景）；`<` 注入防护由 `BuildSystemPrompt` 的 `sanitize()` 统一执行（`prompt.go:126`）。
- **空值语义**：`{"SOUL.md":""}` 与缺省 `SOUL.md` 键等价——该文件继承团队默认（逐文件覆盖仅对非空值生效）。

### 4.3 size 约束

与 loader 完全一致，PATCH 入口强制校验：

| 约束 | 值 | 常量 |
|---|---|---|
| 单文件 | ≤ 8 000 chars | `agentconfig.MaxFileChars`（`loader.go:34`） |
| 总量 | ≤ 40 000 chars | `agentconfig.MaxTotalChars`（`loader.go:37`） |

> **合并截断提示**：上表约束由 PATCH 入口 `ValidateOverrides` 校验 override 自身。合并 team default 后总量可能超 `MaxTotalChars`（team default 单文件可达 ~39000 chars），此时 `LoadForWorkspace` 的 `enforceTotalLimit` 按字段顺序静默截断（`slog.Warn`）。PATCH 侧无法预知 team default 大小，该截断对用户不可见——属 defense-in-depth 权衡（见 §5 `LoadForWorkspace`）。

---

## 5. 两层继承解析（LoadForWorkspace）

### 5.1 函数签名（新增，`internal/agentconfig/loader.go`）

```go
// LoadForWorkspace resolves agent configs for the WebChat track using two-level
// inheritance: team defaults (from dir) → workspace overrides (DB JSON).
//
// Team defaults are loaded via Load with botName="" (WebChat sessions don't select
// a bot, spec ① §2.4). Each non-empty override entry replaces the corresponding
// team-default field. injectExclude takes highest priority: an excluded file is
// never injected even if overridden.
//
// The Message Channel track continues to call Load directly; this function is
// WebChat-only and never alters Load's behavior.
func LoadForWorkspace(dir, platform string, overrides map[string]string, injectExclude ...string) (*AgentConfigs, error)
```

### 5.2 解析逻辑

```
1. base := Load(dir, platform, "", injectExclude...)     // 团队默认，复用现有三级 fallback
2. for file ∈ configFiles (SOUL/AGENTS/SKILLS/USER/MEMORY):
     v := overrides[file]
     if v != "" && !shouldExclude(file, injectExclude):
         base.<field> = v                                  // 逐文件覆盖
3. return base
```

- **白名单过滤**：overrides 中不在 `configFiles` 的键静默忽略（防御性，PATCH 入口已拒未知键，此处为纵深防御）。
- **exclude 优先级最高**：被 `injectExclude` 命中的文件，即使 override 了也不注入（与现有 `Load` 的 exclude 语义一致，`loader.go:72-75`）。
- **产出直接喂 `BuildSystemPrompt`**：组装逻辑零改动，META-COGNITION 仍由 `buildHotplexMetacognition()` 独立注入首位（§9）。

### 5.3 错误降级

- `Load`（团队默认）失败：返回 error（与现有行为一致，由 `injectAgentConfig` 记日志并跳过注入）。
- overrides 解析在调用方完成（见 §7），`LoadForWorkspace` 收到的已是 `map[string]string`，无 JSON 解析失败路径。

---

## 6. 双轨隔离

| 轨 | 解析路径 | 触发条件 |
|---|---|---|
| **WebChat** | `LoadForWorkspace(dir, "webchat", overrides, exclude...)` | `workspaceOverrides != nil` |
| **Message Channel**（Slack/Feishu） | `Load(dir, platform, botName, exclude...)`（不变） | `workspaceOverrides == nil` |

**隔离保证**：
- `Load` 函数体、签名、所有现有调用方（Slack/Feishu 适配器、cron executor 等）零改动。
- `LoadForWorkspace` 是 WebChat 专属新增，内部以 `Load` 为底座，不引入任何 Message Channel 会走的代码路径。
- 单元测试须显式回归 `Load` 行为不变（§11）。

---

## 7. 数据流打通

### 7.1 注入时机：每次 worker 启动

与现有 `Load` 一致——`injectAgentConfig` 在每次 `createAndLaunchWorker`（`bridge_worker.go:82`）调用时执行，覆盖**新建**与 **resume 重启**两条路径。因此 workspace overrides 每次 worker 启动重新解析，天然反映最新配置（配置仅在 session 初始化 / `/reset` 生效，运行中改不立即生效的语义不变，见 CLAUDE.md「配置热更新」）。

### 7.2 `workerLaunchParams` 扩展

```go
type workerLaunchParams struct {
    ctx                context.Context
    wt                 worker.WorkerType
    workerInfo         worker.SessionInfo
    platform           string
    botID              string
    botName            string
    forwardOpts        *forwardOpts
    injectExclude      []string
    workspaceOverrides map[string]string // 新增：WebChat 轨 workspace 配置覆盖；nil = Message Channel 轨走 Load
}
```

### 7.3 Bridge 统一解析 overrides（helper）

代码精读发现：`createAndLaunchWorker` 有 **3 个调用点**——StartSession（`bridge.go:189`）、resume（`bridge.go:297`）、fresh-start（`bridge_worker.go:213`）。其中 resume 与 fresh-start 在 **Bridge 内部触发**（worker 崩溃恢复 / 重连），不经 `api.go`。故 overrides 解析必须由 Bridge 自己完成，不能依赖外部调用方（如 `CreateSession`）传入。

方案：Bridge 新增**窄接口**依赖 + helper，3 点统一调用：

- **窄接口** `WorkspaceOverridesReader`：仅需 `GetWorkspaceByID(ctx, id) (*session.Workspace, error)`（复用 spec ① `WorkspaceReader` 思路，`api.go:50`）。
- **`BridgeDeps.WSStore`**（`deps.go:22` 加字段）：`gateway_run.go:260` NewBridge 传 `deps.WorkspaceStore`（spec ① 已有，`gateway_run.go:78`）。
- **helper `b.resolveWorkspaceOverrides(workspaceID string) map[string]string`**：`workspaceID == ""` → `nil`（Message Channel 轨）；否则查 WSStore → `ValidateOverrides`（§8.2）解析 → 失败 `log.Warn` + 返回 `nil`（降级团队默认，不阻断 worker 启动）。
- **3 个调用点统一**：`workspaceOverrides: b.resolveWorkspaceOverrides(<wid>)`，其中 `<wid>` 取自 `p.WorkspaceID`（StartSession，`worker.SessionStartParams.WorkspaceID` 已有，`worker.go:106`）或 `si.WorkspaceID`（resume/fresh-start，`session.SessionInfo.WorkspaceID` 已有，`manager.go:227`）。

**`api.go CreateSession` 无需改动**：workspace 查询已在（`api.go:248`），`p.WorkspaceID` 已填，overrides 解析集中在 bridge helper。

### 7.4 `injectAgentConfig` 分流

```go
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botName, botID string, injectExclude []string, workspaceOverrides map[string]string) {
    // ... 现有 agentConfigDir == "" / exclude 校验不变 ...
    var configs *agentconfig.AgentConfigs
    if workspaceOverrides != nil {
        configs, err = agentconfig.LoadForWorkspace(b.agentConfigDir, platform, workspaceOverrides, injectExclude...)
    } else {
        configs, err = agentconfig.Load(b.agentConfigDir, platform, botName, injectExclude...)
    }
    // ... 现有 BuildSystemPrompt → info.SystemPrompt 不变 ...
}
```

`createAndLaunchWorker:82` 透传 `params.workspaceOverrides`。

### 7.5 设计选择：workerLaunchParams 携带已解析 map

`workerLaunchParams.WorkspaceOverrides` 是**已解析的 `map[string]string`**（由 §7.3 helper 填充），而非 workspaceID。这保持 `injectAgentConfig` **纯函数**（不查 store、不解析 JSON，易测试）——store 查询与 JSON 解析集中在 bridge helper，`injectAgentConfig` 只做「有 overrides → `LoadForWorkspace`，无 → `Load`」的分流。

---

## 8. PATCH 校验与 API

### 8.1 三层校验（当前 `workspace_handlers.go:162-163` 零校验）

`PATCH /api/workspaces/{id}` 的 `agent_config_overrides` 字段补充：

1. **JSON 解析**：`json.Unmarshal([]byte(raw), &m)` 失败 → 400 `INVALID_CONFIG_JSON`。
2. **键白名单**：遍历 `m`，键不在 `agentconfig.KnownFiles()`（`loader.go:124`）→ 400 `UNKNOWN_CONFIG_FILE`（含未知键列表）。
3. **值类型 + size**：值非 `string` → 400；单值 `len > MaxFileChars` 或总量 `sum > MaxTotalChars` → 400 `CONFIG_TOO_LARGE`。

校验通过后存原始 JSON 字符串（`UpdateWorkspace` 原样存，不变）。

### 8.2 校验函数位置

新增 `internal/agentconfig/validate.go`（或 loader.go 内）：

```go
// ValidateOverrides parses raw JSON and validates keys/types/sizes.
// Returns the parsed map on success, or an error describing the first violation.
func ValidateOverrides(raw string) (map[string]string, error)
```

`workspace_handlers.go` PATCH 调用它；`CreateSession`/resume 路径的解析也复用（统一入口，避免重复逻辑）。

### 8.3 API 契约

`PATCH /api/workspaces/{id}` body（spec ① 已有，本 spec 补校验）：

```json
{ "agent_config_overrides": "{\"SOUL.md\":\"...\",\"USER.md\":\"...\"}" }
```

- 字段是 **JSON 字符串**（嵌套 JSON），与 `Workspace.AgentConfigOverrides string` 存储一致。
- **清除语义（两层，须区分）**：
  - **字段层级**（整个 `agent_config_overrides`）：空 JSON 对象 `"{}"` = 清除所有覆盖（全继承团队默认）；省略字段或空字符串 `""` = 不更新（保持原值，与 spec ① PATCH `if req.AgentConfigOverrides != ""` 语义一致，`workspace_handlers.go:162`）。
  - **文件层级**（JSON 内单个键）：`{"SOUL.md":""}` 与缺省 `SOUL.md` 键等价——该文件继承团队默认（见 §4.2、§5.2，逐文件覆盖仅对非空值生效）。

---

## 9. META-COGNITION 约束与安全

### 9.1 META-COGNITION 不可覆盖（物理保证）

- `META-COGNITION.md` 经 `prompt.go:9` go:embed，在 `BuildSystemPrompt` 内由 `buildHotplexMetacognition()`（`prompt.go:116`）独立注入为 B 通道 `<hotplex>` 首位。
- 它**不在 `configFiles` 白名单**（`loader.go:121` 仅 5 文件），故：
  - PATCH 校验阶段：`META-COGNITION.md` 键被白名单拒绝 → 400。
  - `LoadForWorkspace` 阶段：即使 overrides 含该键，白名单过滤静默忽略。
- workspace overrides 只能改 `AgentConfigs` 的 5 字段，无法触及 META-COGNITION。

### 9.2 B/C 通道约束

- `BuildSystemPrompt` 的 B/C 通道组装逻辑（`prompt.go:33-83`）零改动。
- workspace 可覆盖 B 通道文件（SOUL/AGENTS/SKILLS）与 C 通道文件（USER/MEMORY），但 B 通道「无条件覆盖 C 通道」的冲突法则由 `BuildSystemPrompt` 的 XML 结构（`<directives>` 先于 `<context>`）保证，不受 overrides 来源影响。

### 9.3 XML 注入防护

所有 overrides 值经 `sanitize()`（`prompt.go:126`）转义保留标签（`agent-configuration`/`directives`/`persona` 等，`prompt.go:118-121`），与现有文件内容同等防护。

---

## 10. 错误码

沿用项目 `AppError{Code,...}` 模式（`.agents/rules/golang.md`）。新增：

| Code | HTTP | 场景 |
|---|---|---|
| `INVALID_CONFIG_JSON` | 400 | `agent_config_overrides` 非合法 JSON |
| `UNKNOWN_CONFIG_FILE` | 400 | overrides 含非白名单键 |
| `CONFIG_TOO_LARGE` | 400 | 单文件 > 8KB 或总量 > 40KB |
| `INVALID_CONFIG_VALUE` | 400 | overrides 值类型非 string |

配额/归属错误复用 spec ① 已有码（`WORKSPACE_FORBIDDEN` 等）。

---

## 11. 测试策略

遵循项目规范（`CLAUDE.md` + `.agents/rules/golang.md`）：table-driven + `testify/require` + `t.Parallel()`，单模块 ≤5s（`-count=1 -race`），禁 `time.Sleep`。

### 11.1 单元测试

| 模块 | 要点 |
|---|---|
| `LoadForWorkspace` | 全覆盖（5 文件全 override）/ 部分覆盖（仅 SOUL）/ 全继承（overrides 空）/ exclude 与 override 交互（exclude 优先）/ 白名单过滤（未知键忽略）/ 空值不覆盖 |
| `ValidateOverrides` | 合法 JSON / 非法 JSON / 未知键 / 值类型错误 / 单文件超 8KB / 总量超 40KB / 空字符串 |
| `Load` 回归 | 现有三级 fallback 测试零改动通过（双轨隔离证据） |
| `BuildSystemPrompt` | overrides 经 LoadForWorkspace 产出后，META-COGNITION 仍首位、B/C 结构正确 |

### 11.2 集成测试（隔离核心）

构造两个 workspace 各自不同 overrides，断言：
- 各自 session 的 `info.SystemPrompt` 反映各自 overrides。
- 一个 workspace 改 overrides 不影响另一个。
- workspace 无 overrides 时注入纯团队默认。
- Message Channel 轨 session（`workspaceOverrides=nil`）注入与 spec ① 完全一致。

### 11.3 PATCH 校验测试

- 合法 flat map → 200 + 存储正确。
- 非法 JSON / 未知键（如 `META-COGNITION.md`、`foo.md`）/ 超 size / 值类型错误 → 对应 400 + DB 未修改。

---

## 12. 受影响文件清单

| 文件 | 改动类型 |
|---|---|
| `internal/agentconfig/loader.go` | 新增 `LoadForWorkspace` + `applyOverrides` |
| `internal/agentconfig/validate.go`（新） | `ValidateOverrides`（JSON/键/类型/size 校验） |
| `internal/agentconfig/loader_test.go`（扩展） | `LoadForWorkspace` + `ValidateOverrides` + `Load` 回归测试 |
| `internal/gateway/deps.go` | `BridgeDeps` 加 `WSStore WorkspaceOverridesReader` 字段 |
| `internal/gateway/bridge.go` | 新增 `WorkspaceOverridesReader` 接口 + `resolveWorkspaceOverrides` helper；StartSession(:189)/resume(:297) 调用点填 `workspaceOverrides` |
| `internal/gateway/bridge_worker.go` | `workerLaunchParams` 加 `workspaceOverrides`；fresh-start(:213) 调用点填；`injectAgentConfig` 签名 + 分流；`:82` 透传 |
| `cmd/hotplex/gateway_run.go` | NewBridge 调用(:260) 传 `WSStore: deps.WorkspaceStore` |
| `internal/gateway/workspace_handlers.go` | PATCH `agent_config_overrides` 加三层校验 |
| `internal/gateway/workspace_handlers_test.go`（扩展） | PATCH 校验测试 |

**无新迁移**（复用 spec ① `017_multitenancy_tables.sql:23` 的 `agent_config_overrides` 列）。

（精确行号在实现计划 phase 细化。）

---

## 13. 后续 spec 路线

本 spec（spec ②）落地后，按路线图 §3 阶段 B/C 推进：

- **spec ③**（workspace 级 worker 选择）：与 spec ② 共享 `workerLaunchParams` 的 workspace 数据流扩展（本 spec 搭建 workspace 配置注入管道，spec ③ 的 `worker_preference` 可复用同一打通路径）。`CreateSession` 已消费 `worker_preference`（`api.go:266-276`），spec ③ 主要补白名单校验。
- **spec ④**（OAuth/SSO）：独立，与本 spec 无依赖。
- **spec ⑥**（前端一等公民化）：消费本 spec 的 `agent_config_overrides` PATCH API，提供 workspace 配置编辑 UI。

---

## 附录 A：与 spec ① 的衔接

| spec ① 预留点 | spec ② 消费方式 |
|---|---|
| `workspaces.agent_config_overrides` 列 | 存 flat map JSON |
| `Workspace.AgentConfigOverrides` 字段 | store 原样存取，PATCH 校验后写入 |
| workspace PATCH 接受该字段 | 补三层校验（§8） |
| `workerLaunchParams` 无 workspace 维度 | 加 `workspaceOverrides map`（§7.2） |
| `injectAgentConfig` 无 workspace 分流 | 加 `LoadForWorkspace` 分支（§7.4） |

## 附录 B：固化参数（非 Open Question）

1. **JSON 结构**：flat map（文件名 → 内容），已定（§3）。
2. **size 上限**：复用 `MaxFileChars=8000` / `MaxTotalChars=40000`，已定。
3. **未知键策略**：PATCH 拒绝（400），`LoadForWorkspace` 忽略（纵深防御），已定。
4. **空值语义**：等价缺省，继承团队默认，已定。
