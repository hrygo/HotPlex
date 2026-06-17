# WebChat 多租户 spec ③：workspace 级 worker 选择

**日期**: 2026-06-17
**状态**: Draft（待实现计划）
**分支**: main · **基线**: spec ② 已合入（[PR #748](https://github.com/hrygo/hotplex/pull/748)，`03e8bffa`）
**范围**: WebChat 轨 workspace 级选择 worker type（claude_code/opencode_server/codex_cli/acp），两层 fallback（workspace.worker_preference → 团队默认），白名单校验闭环，双轨隔离 Message Channel 轨零改动
**关联设计**: [`WebChat-Multitenancy-Foundation-Design-Spec.md`](./WebChat-Multitenancy-Foundation-Design-Spec.md)（spec ①）、[`WebChat-Multitenancy-PerWorkspace-AgentConfigs-Design-Spec.md`](./WebChat-Multitenancy-PerWorkspace-AgentConfigs-Design-Spec.md)（spec ②）、[`WebChat-Multitenancy-Roadmap-Spec.md`](./WebChat-Multitenancy-Roadmap-Spec.md) §4 spec ③

---

## 目录

- [1. 背景与现状](#1-背景与现状)
- [2. 目标与非目标](#2-目标与非目标)
- [3. 关键决策汇总](#3-关键决策汇总)
- [4. 白名单校验（ValidateType）](#4-白名单校验validatetype)
- [5. fallback 链（spec ① 已实现）](#5-fallback-链spec-已实现)
- [6. 双轨隔离](#6-双轨隔离)
- [7. PATCH + CreateSession 校验](#7-patch--createsession-校验)
- [8. 会话连续性](#8-会话连续性)
- [9. 错误码](#9-错误码)
- [10. 测试策略](#10-测试策略)
- [11. 受影响文件清单](#11-受影响文件清单)
- [12. 后续 spec 路线](#12-后续-spec-路线)

---

## 1. 背景与现状

spec ①（地基）已为 spec ③ 预留并实现了大半基础设施：

| 预留点 | 现状 | 证据 |
|---|---|---|
| `workspaces.worker_preference` 列 | 已建 + 存取（spec ③ fills 注释） | `internal/session/multitenancy_store.go:19/111/247` |
| CreateSession fallback 链 | 已实现：body/query > `workspace.WorkerPreference` > default | `internal/gateway/api.go:266-272` |
| worker.Type 4 常量 | claude_code / opencode_server / codex_cli / acp | `internal/worker/worker.go:84-87` |
| `RegisteredTypes()` API | 返回所有 `init()` Register 的 worker type | `internal/worker/registry.go:67` |
| `DeriveSessionKey` 含 worker_type | 切 worker = 新 session key | `internal/session/key.go:52` |

**缺口**：PATCH `worker_preference`（`workspace_handlers.go:179-181`）与 CreateSession body/query `worker_type`（`api.go:267-272`）**均无白名单校验**。非法 worker_type 会落到 worker launch 阶段才失败（registry 找不到 Builder），错误信息不友好且延迟到启动期。

**核心结论**：spec ③ 是 spec ① 的收尾——补白名单校验闭环（写入侧 + 请求侧统一），加双轨隔离回归测试，明确会话连续性行为。不新增字段、不改 fallback 链、不动 Message Channel 轨。

---

## 2. 目标与非目标

### 2.1 目标（本 spec 范围）

1. **workspace 级 worker 选择**：WebChat 轨每个 workspace 设 `worker_preference`，新会话用该 worker（除非请求显式覆盖）。
2. **白名单校验闭环**：PATCH `worker_preference` + CreateSession body/query `worker_type` 都校验合法（4 类注册 worker），非法即 400。
3. **双轨隔离**：Message Channel 轨（Slack/Feishu）的 `config_defaults.propagatePlatform` worker_type fallback 链零改动。
4. **会话连续性明确**：切 `worker_preference` 后，新会话用新 worker（新 session key），已存在会话不变——文档化此行为。

### 2.2 非目标（明确排除）

| 项 | 归属 |
|---|---|
| WebChat 前端 worker 选择 UI | spec ⑥ |
| per-user worker 选择 | 永不（spec ① §2.4 拍板两层，无 per-user） |
| Message Channel 轨引入 workspace preference | 永不（双轨完全隔离） |
| Worker 凭证管理 / 切换 | 永不（凭证由 worker 二进制自管，spec ① §2.5） |
| workspace 级 agent-configs 自定义 | spec ②（已合入） |

---

## 3. 关键决策汇总

| 决策点 | 选择 | 理由 |
|---|---|---|
| 白名单源 | **`RegisteredTypes()` 动态** | 新 worker `init()` Register 后自动纳入白名单，无需手改校验代码；`TypeUnknown` 哨兵不 Register，自动排除。DRY，单一真相源 |
| 校验范围 | **PATCH + CreateSession 统一** | 复用 `ValidateType`，彻底防非法 worker_type 进系统（CreateSession 当前 `worker.WorkerType(body.WorkerType)` 零校验是 spec ① 遗留） |
| 校验函数位置 | **`worker.ValidateType`**（worker 包） | 紧邻 `WorkerType` 定义（`worker.go:81`）+ `RegisteredTypes()`（`registry.go:67`），自洽不跨包 |
| 空值语义 | **空合法**（继承默认） | 两层 fallback，与 spec ② agent-configs 空值继承一致 |
| fallback 链 | **body/query > preference > default（不变）** | spec ① 已实现（`api.go:266-272`），spec ③ 只加校验不改链 |
| 会话连续性 | **切 preference = 新 session**（不特殊处理） | `DeriveSessionKey` 含 worker_type，新 key 自然产生新会话；文档化即可 |

---

## 4. 白名单校验（ValidateType）

新增 `worker.ValidateType`，放在 worker 包（紧邻 WorkerType/RegisteredTypes 定义）。下方为示意伪代码，**以代码为准**（实现用锁内 `registry` map 直查而非 `for range RegisteredTypes()`，零分配，见 `registry.go`）：

```go
// internal/worker/worker.go（或 registry.go）

var ErrInvalidWorkerType = errors.New("invalid worker type")

// ValidateType returns nil for an empty wt (means "inherit default") or for any
// registered worker type. A non-empty unregistered type returns ErrInvalidWorkerType.
// Used by both the PATCH workspace handler and CreateSession to reject unknown
// worker types at the boundary instead of at worker launch.
func ValidateType(wt WorkerType) error {
	if wt == "" {
		return nil
	}
	for _, t := range RegisteredTypes() {
		if t == wt {
			return nil
		}
	}
	return fmt.Errorf("%w: %q not in registered types %v", ErrInvalidWorkerType, wt, RegisteredTypes())
}
```

- **空值合法**：空 `worker_preference`/`worker_type` = 继承默认（两层 fallback 语义）
- **非空必须 ∈ RegisteredTypes()**：TypeUnknown 哨兵不 Register，自动排除
- **sentinel + %w 包装**：对齐项目错误规范（errorlint）

---

## 5. fallback 链（spec ① 已实现）

WebChat 轨 CreateSession（`api.go:266-272`）现有 fallback：

```
1. body.worker_type   （请求显式，最高优先）
2. query.worker_type  （legacy 调用方）
3. workspace.WorkerPreference  （DB，spec ③ 核心）
4. default "claude_code"        （config_defaults:128）
```

**spec ③ 不改 fallback 链**，只在步骤 1-3 解析出非空 wt 后、落入步骤 4 前，加 `ValidateType` 校验。

---

## 6. 双轨隔离

| 轨 | worker_type 来源 | spec ③ 影响 |
|---|---|---|
| **WebChat** | CreateSession fallback（body/query > preference > default） | 加白名单校验 |
| **Message Channel**（Slack/Feishu） | `config_defaults.propagatePlatform`（Bot > 平台 > messaging > 默认） | **零改动** |

**隔离保证**：
- Message Channel 轨完全不查 `worker_preference`（无 workspace 概念，会话不选 Bot 见 spec ① §2.4，但 worker_type 走 config 5 级 fallback）。
- WebChat `worker_preference` 只影响 WebChat 会话的 CreateSession。
- `config_defaults.propagatePlatform`（`config_defaults.go:209-212`）与 spec ③ 代码路径无交集。

---

## 7. PATCH + CreateSession 校验

### 7.1 PATCH workspace worker_preference（`workspace_handlers.go:179-181`）

```go
if req.WorkerPreference != "" {
	if err := worker.ValidateType(worker.WorkerType(req.WorkerPreference)); err != nil {
		writeAppError(w, http.StatusBadRequest, "INVALID_WORKER_TYPE", err.Error())
		return
	}
	ws.WorkerPreference = req.WorkerPreference
}
```

### 7.2 CreateSession worker_type（`api.go:266-272`）

```go
wt := worker.WorkerType(body.WorkerType)
if wt == "" {
	wt = worker.WorkerType(r.URL.Query().Get("worker_type"))
}
if wt != "" {
	if err := worker.ValidateType(wt); err != nil {
		writeAppError(w, http.StatusBadRequest, "INVALID_WORKER_TYPE", err.Error())
		return
	}
}
// 后续 fallback 到 ws.WorkerPreference 时再 ValidateType（防御旁路写/存量脏数据，
// review P2）：非法则 warn + 降级 default，不 400（非请求侧错误）、不带到 worker
// launch；最后空则用 TypeClaudeCode default。见 api.go 实现。
```

---

## 8. 会话连续性

`DeriveSessionKey(ownerID, wt worker.WorkerType, clientKey, workspaceID, workDir)`（`key.go:52`）将 worker_type 纳入 session key 派生。

切 `workspace.worker_preference` 的行为：

| 会话 | 行为 |
|---|---|
| 已存在会话 | **不变**——session key 已定，worker 继续跑直到 idle/terminate |
| 新会话（切 preference 后创建） | 用新 worker_type 派生**新 session key** → 新会话，不继承旧 worker 上下文 |

无需特殊处理（DeriveSessionKey 自然产生新 key）。**文档化**：spec 此节 + PATCH 响应可选提示「切换 worker 后，新会话将使用新 worker；已存在会话不受影响」。

---

## 9. 错误码

| 场景 | HTTP | code | sentinel |
|---|---|---|---|
| PATCH 非法 worker_preference | 400 | `INVALID_WORKER_TYPE` | `worker.ErrInvalidWorkerType` |
| CreateSession 非法 worker_type | 400 | `INVALID_WORKER_TYPE` | `worker.ErrInvalidWorkerType` |

---

## 10. 测试策略

table-driven + `t.Parallel()` + `-race`，单模块 ≤5s：

- **TestValidateType**：空合法 / 4 类（claude_code/opencode_server/codex_cli/acp）合法 / `TypeUnknown` 拒 / 大小写敏感（"Claude_Code" 拒）。
- **TestPATCHWorkspaceWorkerPreference**：合法更新 / 非法 400 / 空值不变（保持原 preference）。
- **TestCreateSessionWorkerTypeValidation**：body 合法 / body 非法 400 / query 合法 / query 非法 400 / 空 fallback 到 preference。
- **TestDualTrackIsolation**：Message Channel 轨 worker_type fallback（config_defaults）不受 WebChat preference 影响（回归）。
- **TestSwitchPreferenceNewSession**：切 preference → `DeriveSessionKey` 产生新 key（新会话），旧 key 不变。

---

## 11. 受影响文件清单

| 文件 | 改动 |
|---|---|
| `internal/worker/worker.go`（或 `registry.go`） | + `ErrInvalidWorkerType` + `ValidateType` |
| `internal/worker/registry_test.go`（或 `worker_test.go`） | + `TestValidateType` |
| `internal/gateway/workspace_handlers.go` | PATCH `worker_preference` 加 `ValidateType`（:179-181） |
| `internal/gateway/workspace_handlers_test.go` | PATCH 白名单测试 |
| `internal/gateway/api.go` | CreateSession `worker_type` 加 `ValidateType`（:267-272） |
| `internal/gateway/api_test.go`（或新建） | CreateSession 白名单测试 |

**不改**：`multitenancy_store.go`（字段/存取 spec ①）、`config_defaults.go`（Message Channel 轨）、`key.go`（DeriveSessionKey）、`api.go` fallback 链逻辑。

---

## 12. 后续 spec 路线

spec ③ 合入 → spec ④（OAuth/SSO provider）+ spec ⑤（配额增强）→ spec ⑥（前端 UI，消费 `worker_preference` 选择 UI）。
