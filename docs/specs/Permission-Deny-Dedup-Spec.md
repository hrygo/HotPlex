# Permission Deny Dedup Spec

> 状态：草案 · 分支：`feat/862-reliable-interaction-delivery` · 关联：#862 · 2026-07-10

## 1. 背景与问题

### 1.1 症状

飞书卡片授权请求，用户**点一次"拒绝"后，几秒内同一工具的同一调用立即再次弹出授权卡片**，形成"拒绝→重弹→再拒绝"循环。

### 1.2 根因

HotPlex 整条链路对权限拒绝**零缓存**——deny 是 per-request-id 的一次性信号，任何层都不记录"用户已拒绝此工具/此调用"。

完整循环：

```
agent 调用工具 T(args)
  → worker 发 PermissionRequest(reqID=A)            → 飞书卡片 A
用户点"拒绝"
  → deny 送达 agent（allowed=false）
  → InteractionManager.CompleteClaimed 删除 A       ← 拒绝决定随之消失
agent（LLM 自主）几秒内再次尝试 T(相同 args)
  → worker 发 PermissionRequest(reqID=B)            ← 新 request_id
  → HotPlex 无"上次拒过 T(args)"的记忆 → 发新卡片 B
用户再拒绝 → 循环
```

### 1.3 证据链（deny 投递路径本身是正确的，不是 routing bug）

| 步骤 | 位置 |
|---|---|
| 飞书点"拒绝" | `internal/messaging/feishu/card_action.go:69 cardActionDeny` |
| 构造 deny metadata | `messaging.BuildPermissionResponse(reqID, false, "user denied")` |
| 投递 envelope | `internal/messaging/interaction.go:45 SendInteractionResponse`（Type=Input + metadata.permission_response） |
| gateway 路由 | `internal/gateway/handler.go:128 handleInput → :157 tryInteractionResponse`（在 `deliverToWorker` 前拦截，不误当 user turn） |
| 派发到 worker | `w.Input(ctx, "", md)` → `internal/worker/base/metadata.go:43 DispatchMetadata → :50 HandlePermissionResponse` |
| 送达 agent | claudecode `control.go:137 SendPermissionResponse` 写入 `{allowed:false, reason}`；codex `manager.go:796` 返回 `{scope:"turn", permissions:{}}` |

**关键证据**：`InteractionManager.Register`（`interaction.go:174`）的去重只对**同一 request_id** 生效；codex 的 `scope:"turn"`（`manager.go:824`）直接证明授权作用域是单 turn。全项目 grep `deniedTool|denyMap|permissionCache|autoDeny` 零命中，确认无任何层缓存拒绝。

## 2. 目标与非目标

### 目标

- 用户拒绝一个工具调用后，**60 秒窗口内**同一工具 + 同一参数指纹的重试请求被静默压制（不发卡片），agent 收到本地 deny。
- 压制对 4 个 worker（claudecode / codex_cli / opencode_server / acp）统一生效。
- 副作用最小：窗口外、不同工具、不同参数的请求照常询问；用户改主意可在窗口后或通过 `/reset` 恢复。

### 非目标

- 不做会话级永久拒绝（用户明确选了 60s 窗口，保留每次授权意愿）。
- 不改 agent 侧行为（不在 system prompt 引导"别重试"——那是平行手段，本 spec 不覆盖）。
- 不改 InteractionManager 的 claim/release/watchTimeout 状态机。
- 不处理"不同工具的连续请求"（只针对同指纹重复）。

## 3. 设计

### 3.1 核心机制

在 gateway 层（`Bridge`）新增 **会话级指纹去重缓存** `PermissionDenyDedup`：

- 流出（worker → client）：`processForwardedEvent` 见 `PermissionRequest` → 计算指纹 → 查缓存：
  - **命中**（该指纹在本 session 60s 内被拒过）→ 不转发给 client，本地构造 deny 回投 worker。
  - **未命中** → 正常转发；同时记录 `reqID → 指纹` 映射。
- 流入（client → worker）：`tryInteractionResponse` 见 `permission_response` 且 `allowed=false` → 用 `request_id` 反查指纹 → 写入 denied set（60s TTL）。

### 3.2 指纹定义

```
fingerprint = sha256( ToolName + "\x00" + canonicalArgs )[:16]   // 截断，hex 编码

canonicalArgs:
  if len(Args) > 0 and Args[0] 解析为合法 JSON:
      递归排序所有 object 的 key → 紧凑序列化（无多余空白）
  else:
      strings.Join(Args, "\x1f")    // 单元分隔符，避免普通空格碰撞
```

设计要点：
- 同工具不同参数 → 不同指纹（覆盖用户选择"工具+参数指纹"）。
- 同参数不同 JSON key 顺序 → 相同指纹（规范化，避免 agent 重排字段绕过）。
- 无 Args（如某些 worker）→ `canonicalArgs=""`，指纹仍有效，退化为按工具去重。
- JSON 解析失败 → 走 `Join` fallback，不阻断转发。

### 3.3 组件结构

挂在 `Bridge` 上（`Handler.bridge *Bridge` 让 handler 与 forwardEvents 共享同一实例）：

```go
// internal/gateway/permission_dedup.go
type PermissionDenyDedup struct {
    mu          sync.Mutex
    window      time.Duration              // 默认 60s，可配置
    bySession   map[string]*dedupState     // sessionID → state
}

type dedupState struct {
    denied     map[string]time.Time        // fingerprint → 否决过期时刻
    reqIndex   map[string]string           // request_id → fingerprint（流出登记）
}
```

对外方法：
- `RegisterRequest(sessionID, reqID, fingerprint)` — 流出时登记 reqID→指纹；若指纹已在本 session denied 且未过期，返回 `(hit=true)`。
- `RecordDeny(sessionID, reqID)` — 流入 deny 时反查 reqID→指纹，写入 denied set。
- `ClearSession(sessionID)` — session 结束/`/reset` 时整体清理。
- `EvictExpired()` — 周期清理过期项（可选；`RegisterRequest` 惰性清理即可）。

### 3.4 流出拦截（`bridge_forward.go processForwardedEvent`）

在 PermissionRequest 分支、转发给 client 之前插入：

```
on PermissionRequest(env):
    data = ExtractPermissionData(env)          // 复用 messaging.ExtractPermissionData
    fp   = computeFingerprint(data)
    if dedup.RegisterRequest(sessionID, env.ID, fp).hit:
        // 静默压制：不发卡片
        log.Info("permission: suppressing repeated request",
                 "request_id", env.ID, "tool", data.ToolName, "session_id", sessionID)
        metrics.PermissionDedupHits.Add(tool=data.ToolName)
        // 本地 deny 回投 worker
        denyMd := messaging.BuildPermissionResponse(env.ID, false,
                   "previously denied within dedup window")
        _ = w.Input(ctx, "", denyMd)
        return  // 不进入正常转发
    // 未命中：正常转发（现有逻辑），reqID→指纹已由 RegisterRequest 登记
```

### 3.5 流入记录（`handler.go tryInteractionResponse`）

在派发到 worker **之前或之后**（建议之后，确保 deny 先送达 agent 再记缓存）：

```
on permission_response with allowed == false:
    dedup.RecordDeny(env.SessionID, requestID)
```

`RecordDeny` 内部用 `reqIndex[requestID]` 反查指纹，写入 `denied[fp] = now + window`，然后删除该 reqIndex 条目。

> allow 响应：不强制清除缓存（用户选 60s 窗口语义下，allow 只是"这一次允许"，不清除之前的拒绝记忆）。若后续需要"允许后解除该指纹的拒绝"，列为 v2。

### 3.6 会话生命周期

- `Bridge` 在 session GC / `/reset` / terminate 路径已有钩子（见 `bridge.go` Cleanup、`InteractionManager.CancelAll`）——在这些点调 `dedup.ClearSession(sessionID)`，避免内存泄漏与跨 session 串扰。
- 不按 platform 维度隔离，**按 sessionID + OwnerID** 隔离（指纹 key 实际是 `ownerID + "\x00" + fp`，见 §3.7）。

### 3.7 Owner 维度

PermissionRequest 携带 `OwnerID`。指纹 key 前置 OwnerID：

```
cacheKey = ownerID + "\x00" + fingerprint
```

群聊/多用户共享 session 时，A 用户的拒绝不影响 B 用户的同指纹请求。`OwnerID` 为空（部分平台 session）时退化为纯指纹 key。

## 4. 数据流

### 4.1 正常路径（未命中）

```
worker → PermissionRequest(reqID=A, tool=T, args)
  → processForwardedEvent
  → RegisterRequest(sess, A, fp(T,args))  → 未命中
  → 转发给 client → 飞书卡片 A
  → tryInteractionResponse (allowed=false)
  → w.Input(deny) → agent
  → RecordDeny(sess, A) → denied[owner\.fp(T,args)] = now+60s
```

### 4.2 压制路径（命中）

```
worker → PermissionRequest(reqID=B, tool=T, args)   ← 几秒内同指纹
  → processForwardedEvent
  → RegisterRequest(sess, B, fp(T,args))  → 命中（60s 内拒过）
  → 不转发 → 飞书无新卡片
  → w.Input(deny "previously denied within dedup window") → agent
  → agent 收到 deny，继续/停止
```

## 5. 边界与回退

| 场景 | 行为 |
|---|---|
| `ExtractPermissionData` 失败 | 不拦截，正常转发（fail-open） |
| Args 非合法 JSON | 走 `Join` fallback 指纹，仍去重 |
| OwnerID 为空 | 退化为纯指纹 key（平台 session） |
| 指纹计算 panic | `defer recover` + 告警 + 正常转发 |
| 同 session 并发同指纹请求 | `mu` 串行化 RegisterRequest；首个登记，后续命中 |
| session 重置 / GC | `ClearSession` 清空该 session 全部条目 |
| worker 不支持 `Input(ctx, "", md)` 派发 | 所有 4 个 worker 均实现 `HandlePermissionResponse`（`base/metadata.go`），已有路径 |

## 6. 配置

```yaml
worker:
  permission_deny_dedup:
    enabled: true          # 默认 true
    window: 60s            # 默认 60s
```

env 绑定：`HOTPLEX_WORKER_PERMISSION_DENY_DEDUP_ENABLED` / `_WINDOW`（沿用 `config_loader.go BindEnv` 模式）。`Validate` 校验 `window > 0`。

## 7. 可观测性

- **Metric**（`internal/observability`）：
  - `gateway_permission_dedup_hits_total{tool,worker}` — 压制次数
  - `gateway_permission_dedup_denied_entries` — 当前活跃 denied 条目数（gauge）
- **Log**（slog JSON）：
  - 压制时 `INFO`：`permission: suppressing repeated request` + request_id/tool/session_id
  - 登记拒绝时 `DEBUG`：`permission: recorded deny` + fingerprint（截断）/tool

## 8. 测试计划

单元测试（`internal/gateway/permission_dedup_test.go`）：
- 同指纹 60s 内第二次 `RegisterRequest` → hit
- 不同指纹 → 未命中
- `RecordDeny` 后 `RegisterRequest` 命中；窗口过期后未命中（注入时钟）
- `ClearSession` 清空
- OwnerID 不同 → 互不影响
- Args JSON key 顺序不同 → 同指纹
- Args 非 JSON → fallback 指纹稳定
- 并发 `RegisterRequest`（`-race`）

集成测试（`bridge_forward` 现有 harness）：
- mock worker 产出同指纹 PermissionRequest 两次 → 第二次 client 未收到、worker 收到本地 deny
- deny 后再请求不同工具 → 正常转发

## 9. 验收标准

1. 飞书拒绝同一工具同一参数后，60s 内 agent 重试**不再弹出新卡片**，agent 收到 deny 响应。
2. 60s 后同指纹重试**重新弹卡**（用户可重新决策）。
3. 同工具不同参数**照常弹卡**。
4. `/reset` 或 session 结束后，缓存清空，重新弹卡。
5. 现有 InteractionManager claim/timeout 行为不变；非 PermissionRequest 事件转发不受影响。
6. 三平台（linux/macOS/windows）`go test -race` 通过。

## 10. 风险与权衡

| 风险 | 缓解 |
|---|---|
| agent 在 60s 窗口内"合理"重试被误压（如用户其实想改主意） | 60s 是用户选定值，足够短；窗口后恢复询问 |
| 指纹规范化漏掉某种 worker 参数格式 → 不同调用误判同指纹 | fallback 用原始 Join；集成测试覆盖各 worker |
| 缓存内存增长 | TTL 惰性清理 + `ClearSession` + `EvictExpired` 周期清理 |
| 本地 deny 回投与 worker 正常输出读取并发 | `w.Input` 为线程安全设计（claudecode 有 stdin 锁；codex/ocs/acp 经各自 manager） |
| 群聊多用户 | OwnerID 维度隔离 |

## 11. 实现切片建议

1. `PermissionDenyDedup` 组件 + 单元测试（含时钟注入）
2. 指纹计算 + `ExtractPermissionData` 复用
3. `processForwardedEvent` 流出拦截 + 本地 deny 回投
4. `tryInteractionResponse` 流入记录
5. session 生命周期 `ClearSession` 接线
6. 配置 + Validate + env 绑定
7. metric/log
8. 集成测试 + 三平台 `go test -race`

## 12. 跟踪

- GitHub Issue：（创建后回填）
- 相关：#862（跨平台可靠交互投递）
