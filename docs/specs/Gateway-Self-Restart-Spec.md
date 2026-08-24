---
type: spec
tags:
  - project/HotPlex
  - component/Gateway
  - component/Feishu
date: 2026-08-24
status: proposed
progress: 0
---

# 飞书特权命令安全重启 Gateway 与生命周期通知规格

**状态**：待书面审阅（Proposed）  
**更新日期**：2026-08-24  
**追踪 Issue**：[#970](https://github.com/hrygo/hotplex/issues/970)  
**实现范围**：Gateway 控制面、飞书适配器、进程重启协调、生命周期通知  
**不属于**：AEP Worker 控制协议

## 1. 背景与目标

HotPlex 已具备 detached restart helper、Admin API 重启入口和服务停止/启动广播，但尚未形成可从飞书安全触发的完整控制面：

1. 普通飞书消息最终可能进入 Worker。若让 Worker 执行 `hotplex gateway restart`，Gateway 关闭时会终止 Worker 及其进程树，发起重启的执行链可能在启动新实例前被杀死。
2. 飞书现有 `allow_from` 是普通 Bot 使用权限，不足以表达“可重启宿主进程”的高权限身份。
3. detached helper 的冷却检查与标记写入不是一个原子操作，不同群聊并发请求可能同时通过检查。
4. 现有生命周期消息只说明“正在停止/已启动”，不能确认停止和启动分别运行了哪个版本。
5. 启动广播只依赖活跃 Session 快照；如果操作者在尚未建立 Session 的聊天中发出重启命令，新实例可能无法向原聊天回执。

本规格实现以下结果：

- 允许配置的飞书操作者用精确命令 `/gateway restart` 请求 Gateway 重启。
- 命令由 Gateway 在 Worker 之前拦截并执行，Worker 永远不负责重启宿主进程。
- Feishu、Admin API 和 CLI 共享同一套重启互斥与调度语义。
- 旧实例发送“停止中”消息，新实例在消息适配器可用后发送“已启动”消息；两者包含可核对的版本与实例信息。
- 并发请求全局最多产生一个有效重启任务。

## 2. 当前实现基线

以下事实基于 2026-08-24 的 `v1.42.1` / `4c7d9747` 源码：

| 能力 | 当前状态 | 主要位置 |
| --- | --- | --- |
| Detached helper | 已实现；独立进程组、PID/Service 两类重启路径 | `cmd/hotplex/gateway_restart_helper.go` |
| CLI detached restart | 已实现 `hotplex gateway restart --detached` | `cmd/hotplex/gateway_cmd.go` |
| Admin restart | 已实现 `POST /admin/restart`，要求 `admin:write` | `internal/admin/handlers.go`、`cmd/hotplex/routes.go` |
| 生命周期广播 | 已实现停止前保存活跃 Session，启动后恢复并广播 | `cmd/hotplex/lifecycle_broadcast.go` |
| 飞书普通访问控制 | 已实现 DM/群聊策略与普通用户 allowlist | `internal/messaging/gate.go` |
| 飞书宿主重启命令 | 未实现 | — |
| 独立重启操作者 allowlist | 未实现 | — |
| 原子全局重启租约 | 未实现 | — |
| 请求聊天的跨重启回执 | 未实现 | — |

旧版 spec 将 detached helper 标为“全部实现”，同时把 Admin API 写成后续阶段，已与源码不一致。本次修订把 detached helper 视为现有基础，不重复设计其已完成部分。

## 3. 范围边界

### 3.1 包含

- 飞书精确命令识别、独立授权、直接回复与审计。
- Gateway-owned 重启协调器、原子租约和跨进程回执。
- CLI、Admin API、飞书入口统一接入协调器。
- PID、systemd、launchd、Windows SCM 管理模式的重启语义与验证。
- 停止和启动通知的版本、构建、PID、时间及请求关联信息。
- 配置参考、示例配置和测试。

### 3.2 不包含

- 不允许自然语言（如“帮我重启服务”）触发宿主重启。
- 不允许 Worker、Cron prompt 或 AEP `control.*` 直接获得宿主重启权限。
- 不新增或修改 `pkg/events` wire contract。
- 不通过本机回环 HTTP 调用 Admin API。
- 不在本期实现外部告警服务。新 Gateway 无法启动时，只能由 helper/supervisor 记录失败；不得伪造“启动成功”消息。
- 不扩大普通 `allow_from` 的权限含义。

## 4. 架构决策

### D1：宿主命令在 Gateway 中拦截

飞书事件仍按“自消息过滤 → 过期检查 → 去重 → 消息转换 → 普通 Gate”处理。通过普通 Gate 后，先识别保留的宿主命令，再进入现有 `messaging.DetectCommand` 和 Worker 流程。

任何以 `/gateway` 开头的输入都属于保留命令空间：合法命令进入宿主控制处理器；未知子命令或参数返回帮助；无权限返回拒绝。保留命令不得回落到 Worker。

这使 restart helper 的父进程是 Gateway，而不是 Worker。Gateway 后续终止 Worker 进程组时，不会杀死由 Gateway 直接创建并已脱离的 helper。

### D2：重启权限与普通聊天权限分离

新增飞书配置：

```yaml
messaging:
  feishu:
    gateway_restart_allow_from:
      - ou_operator_open_id
    bots:
      - name: ops
        gateway_restart_allow_from:
          - ou_bot_specific_operator
```

规则如下：

- 值必须是飞书 `sender.sender_id.open_id`。
- 平台级默认值为空，即默认禁止所有用户执行重启。
- Bot 字段省略时继承平台值；显式 `[]` 表示该 Bot 禁止所有用户执行。
- 操作者仍必须先通过普通 Gate；该 allowlist 只增加第二道授权，不绕过 Bot 的普通访问策略。
- 平台级环境变量为 `HOTPLEX_MESSAGING_FEISHU_GATEWAY_RESTART_ALLOW_FROM`，格式与现有 `ALLOW_FROM` 一致，使用逗号分隔。
- 配置热更新后，新请求立即使用新 allowlist；已进入重启租约的请求不被撤销。

### D3：所有入口共享 Gateway-owned 重启协调器

新增 `restartCoordinator`，统一承载实例发现、全局互斥、回执持久化和 helper 调度。三个入口只负责各自的认证与响应：

- 飞书：普通 Gate + 操作者 OpenID allowlist。
- Admin API：既有 `admin:write` scope。
- CLI：本机进程权限。

协调器提供两阶段语义：

1. `Prepare`：发现当前实例、原子获取租约、生成 request ID、持久化可选回执，返回 ticket。
2. 调用方先发送“已受理”响应。
3. `Commit`：使用 ticket fork detached helper，并将 helper PID 写回租约。
4. 如果响应发送失败，调用 `Abort` 删除该 ticket 对应的回执和租约，不执行重启。
5. 如果 `Commit` 失败，调用方发送“调度失败”；协调器清理自身创建的回执和租约。

Admin API 先返回既有 `{"status":"restarting"}`，再异步 `Commit`；CLI 直接 `Prepare + Commit`。三者不得保留绕过协调器的独立冷却逻辑。

### D4：原子租约代替“先检查、后写标记”

租约文件固定为 `$HOTPLEX_HOME/.pids/gateway.restart`，使用 `O_CREATE|O_EXCL` 原子创建，权限为 `0600`。

租约最少包含：

```json
{
  "schema_version": 2,
  "request_id": "128-bit-random-id",
  "phase": "prepared|helper_started|waiting_for_ready",
  "owner_pid": 1234,
  "helper_pid": 5678,
  "created_at": "RFC3339Nano"
}
```

约束：

- request ID 使用 `crypto/rand`，不得使用 `math/rand`。
- 只有 ticket/request ID 匹配的调用方可以更新或释放租约。
- 租约存在时，所有入口返回“restart already in progress”及 request ID，不再创建 helper。
- `prepared` 状态若 owner PID 已不存在，可回收；其他状态超过 5 分钟且 owner/helper 均不存在时可回收。
- 回收采用“读取校验 → 删除旧租约 → `O_EXCL` 重试”；最终仍只能有一个请求成功获取租约。
- helper 启动新进程后不立即删除租约；由新 Gateway 达到 ready 状态后完成租约。启动失败时保留租约至过期，避免失败循环。

### D5：跨重启回执不依赖 Session

`Prepare` 为飞书请求写入独立 receipt，文件固定为 `$HOTPLEX_HOME/.pids/gateway.restart.receipt.json`，权限 `0600`，并通过临时文件 + rename 原子替换。

receipt 只保存完成回执所需的最小信息：

```json
{
  "schema_version": 1,
  "request_id": "...",
  "platform": "feishu",
  "bot_name": "ops",
  "platform_key": {"chat_id": "oc_...", "thread_ts": "..."},
  "requested_at": "RFC3339Nano",
  "old_version": "v1.42.1",
  "old_pid": 1234
}
```

不得保存消息正文、凭据、token、配置路径或原始内部错误。操作者 OpenID 进入结构化审计日志，不进入发给普通会话的生命周期消息。

新 Gateway 在以下条件全部满足后处理 receipt：

1. 消息适配器已启动并注册到 Bot registry。
2. Gateway/Admin HTTP server 已启动。
3. 当前进程的 ready 状态已建立。

停止和启动广播都将 receipt 的显式飞书目标与既有 Session 快照目标合并，并按 `platform + bot identity + platform key` 去重。即使请求聊天从未创建 Session，也必须收到停止和启动确认；如果它已在 Session 快照中，每个阶段也只发送一次。

该去重保证单次正常执行路径不会因“Session 目标 + receipt 目标”产生重复。若进程在飞书已接收消息、但本地尚未来得及记录完成状态的窗口内崩溃，后续重试可能产生带相同 request ID 的重复消息；除非飞书发送 API 提供并已验证可用的幂等键，否则不得宣称跨崩溃 exactly-once。

### D6：Service 模式由系统 supervisor 拥有重启事务

不得从 Worker 中执行 stop/start，也不得用消息内容拼接 shell 命令。

| 运行模式 | 执行者 | 规定语义 |
| --- | --- | --- |
| PID/daemon | detached helper | SIGTERM 旧 PID，最多等待 30 秒，必要时强制终止，再启动新 daemon |
| systemd | systemd | `service.Manager.Restart` 提交单次 `systemctl restart` 事务 |
| launchd | launchd | `Restart` 改为 supervisor-owned `launchctl kickstart -k`，不得在旧进程后代中串行 `Stop`/`Start` |
| Windows SCM | detached helper + SCM | 请求 Stop，轮询至 `Stopped`（有界超时），再调用 Start；验证 helper 不属于 Worker Job Object |

不能通过放宽 systemd `KillMode`、禁用 Worker 进程树清理或延长无界 sleep 来“保证”helper 存活。

## 5. 命令与响应契约

### 5.1 命令语法

唯一有效命令：

```text
/gateway restart
```

解析规则：

- 在消息清洗及 @Bot mention 移除后执行 `TrimSpace`。
- 命令名大小写不敏感，但不接受额外参数。
- `/gateway restart now`、`/gateway foo` 等返回宿主命令帮助，不进入 Worker。
- 中文自然语言、Markdown 代码块中的文本和 Worker 输出均不能触发。

### 5.2 响应状态

| 状态 | 发送者 | 含义 |
| --- | --- | --- |
| `denied` | 旧 Gateway | 用户未在专用 allowlist；不创建租约 |
| `conflict` | 旧 Gateway | 已有重启进行中；返回现有 request ID |
| `accepted` | 旧 Gateway | 租约与 receipt 已持久化，尚未停止进程 |
| `stopping` | 旧 Gateway | 正在执行受控关闭 |
| `started` | 新 Gateway | 新实例已 ready；这是唯一成功完成信号 |
| `schedule_failed` | 旧 Gateway | helper 未成功启动；不进入重启 |

helper 启动不等于 Gateway 启动成功，因此旧实例不得发送 `started`。

### 5.3 消息格式

停止消息：

```text
⚠️ HotPlex Gateway 正在停止
版本: v1.42.1 (build 2026-08-24T08:00:00Z)
实例: pid=1234, darwin/arm64
原因: 飞书重启请求 req_abcd1234
时间: 2026-08-24T16:00:00+08:00
```

启动消息：

```text
✅ HotPlex Gateway 已启动
版本: v1.42.2 (build 2026-08-24T09:00:00Z)
实例: pid=5678, darwin/arm64
上次版本: v1.42.1
请求: req_abcd1234
时间: 2026-08-24T16:00:18+08:00
```

普通停止/启动不显示“请求”和“上次版本”字段。版本与构建信息统一来自 `newBuildInfo()` / `versionString()`，不得在消息模板中硬编码。

## 6. 端到端时序

```text
Feishu event
  -> dedup + ordinary Gate
  -> reserved host-command parser
  -> OpenID operator authorization
  -> restartCoordinator.Prepare
       -> atomic global lease
       -> persist restart receipt
  -> direct Feishu accepted reply
  -> restartCoordinator.Commit
       -> spawn detached helper from Gateway
  -> old Gateway BroadcastStopping(version=old)
  -> graceful shutdown
       -> terminate all Workers
  -> helper / supervisor starts new Gateway
  -> adapters + HTTP ready
  -> merge lifecycle snapshot + restart receipt targets
  -> BroadcastStarted(version=new)
  -> complete receipt and release lease
```

关键点：Worker 只在关闭阶段被终止，从未持有重启事务，也没有机会把自然语言解释为宿主命令。

## 7. 错误处理与审计

### 7.1 错误处理

| 场景 | 行为 |
| --- | --- |
| 非操作者发送命令 | 直接拒绝并审计；不进入 Worker |
| 多聊天并发请求 | 首个请求获取租约，其余返回 conflict |
| accepted 回复失败 | Abort；不重启 |
| helper fork 失败 | 清理本请求 receipt/lease，发送 schedule_failed |
| 旧进程 30 秒未退出 | helper 强制终止并继续启动 |
| 新进程启动失败 | helper/supervisor 记录失败；不发送 started；租约等待过期 |
| 新进程 ready，但飞书发送失败 | 记录可重试 receipt；不得删除未成功完成的目标 |
| receipt 损坏或版本未知 | fail closed：隔离坏文件、记录结构化错误，不发送猜测内容 |

receipt 的发送重试必须有界。同一运行实例内按 request ID 与目标去重，成功发送后再完成 receipt；跨崩溃窗口遵循至少一次投递语义，并在消息中保留 request ID 供用户和审计去重。

### 7.2 审计字段

至少记录：

- `action=gateway.restart`
- `request_id`
- `source=feishu|admin|cli`
- `actor`（飞书为 OpenID，日志侧处理个人标识）
- `bot_name`、`chat_id`（适用时）
- `result=denied|conflict|prepared|helper_started|started|failed`
- `old_pid`、`new_pid`、`old_version`、`new_version`（可获得时）

日志不得记录消息正文、App Secret、Admin token、完整环境变量或内部配置路径。

## 8. 实施分解

### Phase 1：命令与专用授权

- 在 `internal/messaging` 增加保留宿主命令解析，不修改 AEP。
- 扩展 `FeishuConfig`、`FeishuBotConfig`、环境变量映射、Bot 配置适配和配置参考。
- 在 `internal/messaging/feishu/handler.go` 中于 Worker 分发前拦截 `/gateway`。
- 未授权、未知子命令和合法命令均不得落入 `handleTextMessage`。

### Phase 2：统一协调器、租约与 receipt

- 新增 `cmd/hotplex/gateway_restart_coordinator.go` 及单元测试。
- 将 `pid.go` 的 restart marker 演进为原子租约，兼容或安全回收 schema v1 文件。
- 新增 receipt store，限制权限并使用原子写入。
- CLI 与 Admin API 改接协调器；移除重复的 check-then-write 路径。

### Phase 3：飞书接线与生命周期消息

- 向 Feishu Adapter 注入最小重启控制接口，不让 adapter 依赖 `cmd/hotplex`。
- 实现 Prepare → accepted → Commit；失败时 Abort。
- 将生命周期文案改为由 `BuildInfo` 和运行实例信息格式化。
- 停止和启动阶段都合并 Session 快照与 receipt 显式目标，并完成阶段内去重与清理。

### Phase 4：Supervisor 语义与跨平台验证

- Linux 保持 systemd 单次 restart 事务并补测试。
- Darwin 将串行 Stop/Start 改为 launchd-owned restart。
- Windows SCM Restart 增加 stopped 状态等待与 helper 生存验证。
- 完成 Linux/macOS/Windows 编译检查和可运行平台集成验收。

## 9. 预计文件变更

| 文件 | 操作 | 责任 |
| --- | --- | --- |
| `internal/config/config_types.go` | 修改 | 专用操作者 allowlist 字段 |
| `internal/config/config_env.go` | 修改 | 平台级环境变量 |
| `internal/config/config_test.go` | 修改 | 默认、YAML、env、继承语义 |
| `cmd/hotplex/bot_config_adapter.go` | 修改 | per-bot 配置读写与热更新 |
| `internal/messaging/operator_command.go` | 新增 | 保留宿主命令解析 |
| `internal/messaging/operator_command_test.go` | 新增 | 精确语法与保留前缀测试 |
| `internal/messaging/feishu/handler.go` | 修改 | Gateway 前置拦截与授权 |
| `internal/messaging/feishu/adapter.go` | 修改 | 控制接口、专用 allowlist、直接回复 |
| `internal/messaging/feishu/*_test.go` | 修改/新增 | 授权、无 Session、禁止 Worker fallback |
| `cmd/hotplex/gateway_restart_coordinator.go` | 新增 | Prepare/Commit/Abort 与统一入口 |
| `cmd/hotplex/gateway_restart_receipt.go` | 新增 | 跨进程请求回执 |
| `cmd/hotplex/gateway_restart_helper.go` | 修改 | 接受 ticket、更新租约、不提前完成 |
| `cmd/hotplex/pid.go` | 修改 | 原子 restart lease 与 v1 兼容 |
| `cmd/hotplex/routes.go` | 修改 | Admin API 复用协调器 |
| `cmd/hotplex/messaging_init.go` | 修改 | 向 Feishu 注入控制接口 |
| `cmd/hotplex/lifecycle_broadcast.go` | 修改 | 动态文案、receipt 目标合并、ready 完成 |
| `internal/service/manager_darwin.go` | 修改 | launchd-owned restart |
| `internal/service/manager_windows.go` | 修改 | 等待 Stopped 后 Start |
| `configs/config.yaml`、`configs/env.example` | 修改 | 安全默认与示例 |
| `docs/reference/configuration.md` | 修改 | 用户配置说明 |

实施计划必须覆盖表中责任；如果源码调查证明某个文件边界不成立，需先修订本规格再实施，不得静默改变安全边界、状态语义或验收标准。

## 10. 测试与验收标准

### 10.1 自动化测试

- Parser table tests：只接受精确命令，所有 `/gateway` 变体均为保留输入。
- Config tests：默认 deny、平台 env、Bot 继承、显式空列表禁用、热更新。
- Feishu handler tests：普通 Gate、专用 allowlist、未知命令、无权限、无 Worker fallback。
- Coordinator tests：Prepare/Commit/Abort、进程发现错误、helper fork 错误、ticket mismatch。
- Lease race tests：至少 50 个并发请求只能有一个成功；使用 `-race`。
- Receipt tests：`0600`、原子写入、schema 校验、损坏文件 fail closed、重试幂等。
- Lifecycle tests：旧/新版本、无 Session 请求者、目标去重、发送失败保留 receipt。
- Service manager tests：systemd 单事务、launchd kickstart、SCM stop-wait-start。
- Build tags：Linux、Darwin、Windows 均完成编译验证。

### 10.2 验收标准

- **AC-1**：未配置 `gateway_restart_allow_from` 时，任何飞书用户都不能重启 Gateway。
- **AC-2**：合法操作者发送 `/gateway restart` 后，消息不创建、不恢复也不调用 Worker。
- **AC-3**：旧 Gateway 的 Worker 全部被终止后，helper 或 supervisor 仍能启动新 Gateway。
- **AC-4**：跨群聊并发 50 次请求只产生一个 helper/重启事务，其余收到 conflict。
- **AC-5**：操作者在没有历史 Session 的聊天中也能收到 stopping 和 started；单次正常重启中每阶段各一条，崩溃重试产生的重复必须携带同一 request ID。
- **AC-6**：停止消息展示旧版本；启动消息展示新版本、PID、OS/Arch 和时间。
- **AC-7**：新 Gateway 未达到 ready 或启动失败时，任何聊天都不会收到 started。
- **AC-8**：Admin API 与 CLI 不能绕过全局租约，并保持现有外部命令/API 兼容。
- **AC-9**：systemd、launchd、Windows SCM 的受管服务路径分别通过平台测试；未通过的平台不得标记支持。
- **AC-10**：`pkg/events`、客户端 SDK 和 AEP 文档无变更。
- **AC-11**：审计可按 request ID 还原 denied → prepared → helper_started → started/failed 链路，且不含凭据或消息正文。
- **AC-12**：`go test -race -shuffle=on` 覆盖的相关模块通过，三平台目标完成编译验证。

## 11. 发布与回退

### 11.1 发布顺序

1. 先发布协调器、原子租约和动态生命周期消息，但保持飞书 allowlist 默认空。
2. 在目标 Bot 配置一个测试操作者，完成 PID 与实际服务模式验收。
3. 再逐步增加生产操作者 OpenID。
4. 观察 request ID 审计链、helper 日志和启动回执后再宣告平台支持。

### 11.2 回退

- 清空 `gateway_restart_allow_from` 可立即禁用飞书重启入口，不影响普通聊天。
- 回退二进制前确认没有 active restart lease；必要时等待租约自然完成或按运维流程人工核对 PID 后清理。
- schema v2 实现必须能识别旧 v1 marker；回退版本遇到新 schema 时应保守拒绝并由人工处理，不能并发启动第二次重启。
- 生命周期 receipt 是附加状态；禁用命令后仍应允许新实例消费并完成已有 receipt。

## 12. 被否决的方案

### 12.1 让 Worker 执行 `hotplex gateway restart --detached`

虽然现有 helper 在 Unix 下会创建独立进程组，但这仍把高权限宿主操作交给自然语言 Worker，无法提供可靠授权、全局互斥和直接回执；Windows Job Object 与服务 supervisor 边界也更难证明。否决。

### 12.2 飞书调用本机 Admin HTTP API

会引入 token 管理、回环网络、双重鉴权和错误映射问题，同时不能解决跨重启 receipt。Gateway 已在进程内，直接复用协调器边界更小。否决。

### 12.3 复用普通 `allow_from`

普通聊天权限不等于宿主运维权限，会把已有用户静默升级为主机操作者。否决。

### 12.4 旧 Gateway 发送“重启成功”

旧实例只能确认请求已受理和正在停止，无法证明新实例 ready。成功消息必须由新 Gateway 发送。否决。

## 13. 完成定义

本功能只有在 AC-1 至 AC-12 全部满足、相关配置文档完成、真实旧 PID 与新 PID 不同且飞书收到新实例回执后，才能从 `proposed` 更新为 `implemented`。仅完成 detached helper、仅看到进程存活或仅通过单元测试都不构成完成。
