# HotPlex Skill 调用：3 渠道 × 4 Worker 实施计划

> 研究范围：WebChat、Slack、Feishu × Claude Code、OpenCode Server、Codex CLI、ACP。本计划已转入实现，本文同时作为设计、验收和交付记录。

## 结论先行

可以在 HotPlex 判断“输入是否命中当前 Skill 列表”，但不能把所有命中结果都改写成同一种 Worker 请求。正确的实现是：Gateway 统一识别，Worker 根据能力选择原生调用方式；没有能力或没有 Worker 权威目录确认时，禁止静默降级为普通模型提示。

| Worker | 当前可确认的原生入口 | HotPlex 当前状态 | 目标适配方式 |
|---|---|---|---|
| Claude Code | `/skill-name [args]` 斜杠文本；CLI 帮助明确 Skills 仍解析该形式 | `Input` 通过 `--input-format stream-json` 原样写入文本；没有 Skill 专用接口 | `text_command`：结构化记录调用，但底层发送规范斜杠文本；需真实账号验收 |
| OpenCode Server | `POST /session/:id/command`，body 含 `command`、`arguments` | 已验证原生接口；当前普通输入固定走 `/message` | `rpc_command`：调用 `/command`，不得走 `/message` |
| Codex CLI | app-server `turn/start` 的 `input` 支持 `{type:"skill",name,path}`，并可配合 `$skill-name args` 文本 | 当前只发送 `{type:"text", text:...}`；本机 0.146.0 schema 已确认 Skill 类型 | `structured_skill`：发送 Skill item + 参数文本；先用 `skills/list` 确认名称和路径 |
| ACP | 协议只规定 Agent 广告 `available_commands_update`，再以 `session/prompt` 普通文本执行 | HotPlex 可透传该更新，但未形成可调用目录；当前 Hermes ACP 只广告内置命令，用户 Skill 不广告，未知 `/` 会落到 LLM | `advertised_command`：仅对当前会话明确广告的命令调用；否则返回“不支持”，不发送普通 Skill 文本 |

## 3×4 入口矩阵

项目的 `internal/e2econtract/manifest.go` 已固定 12 个组合：Feishu、Slack、WebChat 各自连接四类 Worker。三个渠道的普通文本最终都进入 Gateway `handleInput → tryCommandDispatch → deliverToWorker`；因此 Skill 识别和 Worker 能力分派应只放在 Gateway/Worker 层，不能在三个渠道分别复制一套 Skill 解析器。

| 渠道 | Claude Code | OpenCode Server | Codex CLI | ACP |
|---|---|---|---|---|
| WebChat | text_command | rpc_command | structured_skill | advertised_command / unsupported |
| Slack | text_command | rpc_command | structured_skill | advertised_command / unsupported |
| Feishu | text_command | rpc_command | structured_skill | advertised_command / unsupported |

渠道差异只保留在三处：WebChat 自动补全和错误展示、Slack 的 ephemeral/post 错误反馈、Feishu 的回复消息/卡片错误反馈。核心判断必须在 Gateway 统一完成。

## 已完成的本机与官方验证

- HotPlex 固定矩阵已存在，不是 OpenCode 单 Worker：`internal/e2econtract/manifest.go`。
- 当前 Gateway 只识别 help/control/worker 内置命令；未知 `/技能名` 进入普通 `w.Input`，OpenCode 适配器随后固定发 `/message`。
- WebChat、Slack、Feishu 的未知命令都能继续进入同一 Bridge/Gateway 路径；不需要在渠道层新增 Skill 分支。
- OpenCode 1.18.13 隔离服务验证：`/command` 会加载 Skill，`/message` 只保留 `/skill ...` 原文；OpenCode Skill ID 大小写和目录名必须以 Worker 实际列表为准。
- Claude Code 2.1.222 的本机 CLI 帮助明确：Skills 通过 `/skill-name` 解析，`--input-format stream-json` 是当前 HotPlex 启动模式；当前尚未用真实模型完成执行验收。
- Codex CLI 0.146.0 本机生成的 app-server schema 同时包含 `skills/list`、`turn/start` 和 `UserInput.type=skill`，其中 Skill item 必须带 `name`、`path`；当前 HotPlex 仍只发 text item。
- ACP 官方协议规定：Agent 通过 `available_commands_update` 广告命令，调用时仍以 `session/prompt` 的文本块发送 `/name args`。本机 Hermes ACP 只广告 help/model/tools/context/reset/compress/steer/queue/version，未知命令明确返回给 LLM。

## 设计约束

1. 只匹配当前会话可调用目录中的名称。不能对任意 `/xxx` 做前缀猜测。
2. 内置 `/skills`、`/model`、`/reset` 等命令优先于 Skill；必须先完成现有 `tryCommandDispatch`。
3. 支持规范形式 `/skill-id` 和 `/skill-id <args>`。是否兼容无空格形式只能在已知名称中做最长匹配；多重同长命中必须报歧义。
4. 名称大小写不自动转换；Gateway catalog、Worker catalog、实际目录名不一致时返回明确的不可调用错误。
5. 不把 Skill Markdown 正文写入 AEP、execution 或普通 prompt。OpenCode/Codex 由 Worker 原生协议自行加载；Claude/ACP 只发送规范命令文本。
6. 不改变 execution 只保存 SHA-256 payload 指纹的规则。调用名称和参数只作为当前投递的内存结构及受控日志字段存在。
7. Native Skill 失败不得重试成普通文本；否则会把“执行 Skill”变成“让模型猜 Skill”，正是当前问题的高风险路径。

## 分阶段实施

### Task 1：统一 Skill 目录和调用解析

**Files**

- Create: `internal/skills/invocation.go`
- Test: `internal/skills/invocation_test.go`
- Reference: `internal/skills/scanner.go`, `internal/skills/zip.go`

**Implementation**

- 定义 `Invocation{Name, Args}`、`ParseInvocation(content, catalog)`。
- 先排除已有内置命令，再执行完整 token 匹配和已知名称最长前缀匹配。
- 目录项至少保留 canonical name、description、path、来源；不要只用显示名。
- 加入名称冲突、大小写、空白、无空格参数、普通文本、未知 `/xxx` 测试。

**Acceptance**

`rtk go test ./internal/skills -run 'TestParseInvocation' -count=1 -race -shuffle=on`

### Task 2：定义 Worker 能力和权威 Skill Catalog

**Files**

- Modify: `internal/worker/interfaces.go`, `internal/worker/worker.go`
- Modify: all adapters, mock/noop, registry tests as required by the Worker interface rule
- Tests: each Worker package and shared capability tests

**Implementation**

增加可选能力接口，不修改现有 `Worker.Input` 的公共语义：

```go
type SkillInvocation struct {
    Name string
    Args string
    Path string
    Mode SkillInvocationMode
}

type SkillInvoker interface {
    InvokeSkill(context.Context, SkillInvocation) error
}

type SkillCatalogProvider interface {
    ListInvokableSkills(context.Context, string) ([]SkillDescriptor, error)
}
```

能力模式固定为 `text_command`、`rpc_command`、`structured_skill`、`advertised_command`、`unsupported`。不要把 `SkillInvoker` 加入强制 Worker 主接口，避免所有 noop/mock/未来 Worker 被迫伪造能力。

### Task 3：实现四类 Worker 原生适配

**Claude Code**

- `InvokeSkill` 规范化为 `/Name` 或 `/Name Args`，复用现有 stream-json 写入路径。
- 记录 mode=分支，确保恢复时仍按 Skill 调用语义重放。
- 真实验收必须确认 `--print --input-format stream-json` 下 `/known-skill args` 真的触发 Skill，而不是只显示为普通用户文本。

**OpenCode Server**

- 在 `ServerCommander` 增加 `/session/{id}/command` 调用。
- body 为 `command` 和 `arguments`；复用现有 HTTP 错误处理和 SSE 事件链。
- 不把返回的 Skill 正文复制到 HotPlex 输入，不改用 `/message`。

**Codex CLI**

- 先通过 app-server `skills/list` 获取当前 cwd 的权威 Skill 元数据。
- `turn/start` 的 `input` 发送 Skill item `{type:"skill", name, path}`，并发送携带用户参数的 text item（采用 Codex 官方推荐的 `$name args` 标记）。
- 路径必须来自 Codex 的 `skills/list`，不能直接把 HotPlex `.agents/skills` 路径猜给 Codex。
- 更新当前 `TurnInputItem` 序列化、turn replay 和 `turn/start` 请求测试。

**ACP**

- 解析和缓存 session 级 `available_commands_update`，并让命令列表参与当前会话 Skill/Command catalog。
- 仅对 Agent 广告的命令发送 `/name args`；仍使用 ACP `session/prompt`，这是 ACP 的原生命令方式。
- 对 Hermes 当前版本，用户自定义 Skill 不在广告列表中，必须返回明确“不支持当前 ACP Agent 的原生 Skill 调用”，禁止把原文送入 LLM。

### Task 4：Gateway 统一路由 3 个渠道

**Files**

- Modify: `internal/gateway/handler.go`
- Possibly modify: `internal/gateway/worker_cmds.go` for catalog aggregation
- Tests: `internal/gateway/handler_test.go`, `internal/gateway/command_detection_test.go`

**Routing**

```text
Input
  → interaction response
  → existing built-in command
  → ParseInvocation against session catalog
      ├─ no match: existing ordinary Input
      ├─ match + supported: SkillInvoker.InvokeSkill
      └─ match + unsupported/not-authoritative: explicit NOT_SUPPORTED
```

保留 durable accept、InputAck、owner lease、active gate、turn fence、finishOutcome。Skill 只是改变最终 Worker 投递动作，不得绕过可靠投递生命周期。

测试必须用同一组 Skill 场景跑 WebChat、Slack、Feishu × 四 Worker：命中、普通文本、未知 `/xxx`、内置命令优先、参数解析、能力缺失、Worker 投递失败、重复 ID 和恢复。

### Task 5：结构化恢复和重试

**Files**

- Modify: `internal/worker/worker.go`
- Modify: `internal/gateway/bridge.go`, `bridge_forward.go`, `bridge_worker.go`
- Modify each Worker’s `InputRecoverer` implementation as needed

增加内存态 `InputReplay`：普通文本、text command、RPC command、structured skill 分开保存。恢复时必须调用同一种 Worker 能力；绝不把 OpenCode/Codex native Skill replay 成普通 `/message` 或 text-only `turn/start`。

测试要求：成功投递、Worker 进程重启、连接替换、lease 过期、晚到 done、重复输入都不产生二次普通执行。execution/event store 只保存既有指纹和状态，不保存 Skill 正文。

### Task 6：三渠道 UI/错误和观测

- WebChat 选择 Skill 后插入 canonical ID 加一个空格；后端仍是唯一安全边界。
- Slack/Feishu 不在平台 adapter 重复解析；只把 Gateway 的 unsupported/ambiguous 错误转成当前渠道的用户可读反馈。
- 记录 `skill_name`、`worker_type`、`skill_mode`、`dispatch_result`、`platform` 等低敏字段，禁止记录正文、路径中的用户敏感部分和 Skill Markdown。
- `/skills` 列表应展示“可调用/仅可发现/不可用”状态，避免用户选到 Worker 实际不能执行的条目。

### Task 7：3×4 验收和质量门禁

**Focused tests**

```text
rtk go test ./internal/skills ./internal/worker/claudecode ./internal/worker/opencodeserver ./internal/worker/codexcli ./internal/worker/acp ./internal/gateway -count=1 -race -shuffle=on
rtk pnpm --dir webchat test
rtk pnpm --dir webchat lint
```

**Matrix tests**

- 复用 `internal/e2econtract.Combinations()`，要求 12 个 ID 全部执行，不允许循环静默少跑。
- WebChat：4 个 Worker 各跑 Skill/普通文本/内置命令/不支持分支。
- Slack：4 个 Worker 验证原始事件进入同一 Gateway，unsupported 错误通过 ephemeral/post 可见。
- Feishu：4 个 Worker 验证原始事件进入同一 Gateway，unsupported 错误通过回复消息可见。

**Live probes**

1. OpenCode：隔离 cwd + 小写 Skill，验证 `/command` 与 HotPlex 路由。
2. Codex：启动 app-server，验证 `skills/list`、Skill item 和参数 text 的 JSON 形态；不依赖模型执行即可先完成协议验收。
3. Claude：在隔离 Skill + 已认证环境验证 stream-json 下实际 Skill 执行。
4. ACP：用 Hermes 验证当前用户 Skill 被判定 unsupported；再用一个广告自定义命令的 ACP fixture 验证 `available_commands_update → /command args`。

## 交付判定

- 3 个渠道 × 4 个 Worker 的 12 行组合均有测试证据。
- 已知 Skill 命中后不再把 native invocation 静默当普通 prompt。
- OpenCode 走 `/command`，Codex 走结构化 Skill item，Claude 走受控斜杠文本，ACP 只走广告命令。
- 普通文本、未知 `/xxx` 和内置命令保持既有语义。
- Skill 不存在于 Worker 权威目录或能力未广告时，返回明确错误，不触发模型猜测。
- Worker 重启/重试不会改变调用模式。
- 3 个渠道只负责入口和反馈，Skill 识别逻辑只有一份 Gateway 实现。
- Qwen 32K 上下文超限、OpenCode 旧会话复用、插件加载错误仍需单独修复；本方案只解决 Skill 调用语义和错误路由，不承诺消除这些独立问题。

## 推荐实现顺序

先做 Task 1、Task 2 和四 Worker 的能力探针；随后先落 OpenCode/Codex 两条结构化原生路径，再落 Claude text_command 和 ACP advertised_command；最后接 Gateway durable 路由、结构化恢复、3×4 E2E。没有完成 Claude 真实探针和 ACP capability fixture 前，不应把四类 Worker 宣称为“全部支持 Skill”。
