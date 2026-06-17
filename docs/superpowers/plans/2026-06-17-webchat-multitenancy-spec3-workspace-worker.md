# WebChat 多租户 spec ③：workspace 级 worker 选择 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补 spec ① 遗留的 worker_type 白名单校验闭环（PATCH `worker_preference` + CreateSession body/query `worker_type` 统一 `worker.ValidateType`），双轨隔离测试 + 会话连续性文档化。

**Architecture:** spec ① 已实现 worker_preference 字段、CreateSession fallback 链（body/query > preference > default）、worker.Type 4 常量 + `RegisteredTypes()`、`DeriveSessionKey` 含 worker_type。spec ③ 只补白名单校验（新 `worker.ValidateType`，RegisteredTypes 动态源）+ 两个调用点接入 + 双轨隔离/会话连续性回归测试。不改字段/存取/fallback 链/Message Channel 轨。

**Tech Stack:** Go 1.x · testify/require · table-driven · `t.Parallel()` · `-race` · 现有 `internal/worker` registry + `internal/gateway` handlers。

**设计文档:** [`docs/specs/WebChat-Multitenancy-WorkspaceWorker-Design-Spec.md`](../../specs/WebChat-Multitenancy-WorkspaceWorker-Design-Spec.md)（commit `423f8545`）

---

## 文件结构

| 文件 | 责任 | 改动 |
|---|---|---|
| `internal/worker/registry.go` | worker 注册表 + 白名单源 | + `ErrInvalidWorkerType` + `ValidateType` |
| `internal/worker/registry_test.go` | registry 单元测试 | + `TestValidateType` |
| `internal/gateway/workspace_handlers.go` | PATCH workspace | worker_preference 接入 `ValidateType`（:179-181） |
| `internal/gateway/workspace_handlers_test.go` | PATCH 测试 | + 白名单测试（复用 spec ② `patchWorkspace` helper） |
| `internal/gateway/api.go` | CreateSession | body/query worker_type 接入 `ValidateType`（:267-276） |
| `internal/gateway/api_test.go` | CreateSession 测试 | + 白名单测试（复用现有 CreateSession 测试模式） |
| `internal/session/key_test.go` | session key 测试 | + 会话连续性（切 worker_type = 新 key）回归注释/测试 |

---

## Task 1: `worker.ValidateType` + sentinel error（TDD）

**Files:**
- Modify: `internal/worker/registry.go`（追加 `ErrInvalidWorkerType` + `ValidateType`，放在 `RegisteredTypes` 后 :75）
- Test: `internal/worker/registry_test.go`（追加 `TestValidateType`）

- [ ] **Step 1: Write the failing test**

追加到 `internal/worker/registry_test.go` 末尾：

```go
func TestValidateType(t *testing.T) {
	t.Parallel()

	// 4 类已注册 worker（init() Register 由各适配器包完成）
	registered := RegisteredTypes()

	tests := []struct {
		name    string
		wt      WorkerType
		wantErr bool
	}{
		{"empty inherits default", "", false},
		{"claude_code valid", TypeClaudeCode, false},
		{"opencode_server valid", TypeOpenCodeSrv, false},
		{"codex_cli valid", TypeCodexCLI, false},
		{"acp valid", TypeACP, false},
		{"unknown sentinel rejected", TypeUnknown, true},
		{"garbage rejected", WorkerType("bogus_worker"), true},
		{"case sensitive uppercase rejected", WorkerType("CLAUDE_CODE"), true},
		{"case sensitive capitalized rejected", WorkerType("Claude_Code"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateType(tt.wt)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidWorkerType)
				return
			}
			require.NoError(t, err)
		})
	}

	// Sanity: the 4 valid constants are actually in RegisteredTypes()
	// (guards against a future worker package losing its init() Register).
	require.Contains(t, registered, TypeClaudeCode)
	require.Contains(t, registered, TypeOpenCodeSrv)
	require.Contains(t, registered, TypeCodexCLI)
	require.Contains(t, registered, TypeACP)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestValidateType ./internal/worker/... -count=1`
Expected: FAIL — `undefined: ValidateType` / `undefined: ErrInvalidWorkerType`

- [ ] **Step 3: Write minimal implementation**

追加到 `internal/worker/registry.go`（`RegisteredTypes` 函数后，:75 之后）：

```go
// ErrInvalidWorkerType signals a worker type that is not registered.
// Returned by ValidateType for non-empty types absent from RegisteredTypes().
var ErrInvalidWorkerType = errors.New("invalid worker type")

// ValidateType returns nil for an empty wt (means "inherit default") or for any
// registered worker type. A non-empty unregistered type returns ErrInvalidWorkerType.
// Used by both the PATCH workspace handler and CreateSession to reject unknown
// worker types at the boundary instead of at worker launch. See spec ③ §4.
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

确认 `registry.go` 顶部已 import `"errors"` + `"fmt"`（若无需补）。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestValidateType ./internal/worker/... -count=1 -race`
Expected: PASS（含 9 子测试）

- [ ] **Step 5: Commit**

```bash
git add internal/worker/registry.go internal/worker/registry_test.go
git commit -m "feat(worker): ValidateType + ErrInvalidWorkerType (spec ③)"
```

---

## Task 2: PATCH worker_preference 白名单校验（TDD）

**Files:**
- Modify: `internal/gateway/workspace_handlers.go:179-181`
- Test: `internal/gateway/workspace_handlers_test.go`（复用 spec ② `patchWorkspace` helper @140）

- [ ] **Step 1: Write the failing test**

追加到 `internal/gateway/workspace_handlers_test.go`（spec ② 白名单测试附近）：

```go
func TestPATCHWorkspaceWorkerPreference_Whitelist(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.createUserAndLogin(t, "owner", true)
	ws := env.createWorkspace(t, cookie, "owner")

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string // empty = no error envelope
	}{
		{"valid claude_code", `{"worker_preference":"claude_code"}`, http.StatusOK, ""},
		{"valid opencode_server", `{"worker_preference":"opencode_server"}`, http.StatusOK, ""},
		{"valid codex_cli", `{"worker_preference":"codex_cli"}`, http.StatusOK, ""},
		{"valid acp", `{"worker_preference":"acp"}`, http.StatusOK, ""},
		{"empty keeps default", `{"worker_preference":""}`, http.StatusOK, ""},
		{"unknown rejected", `{"worker_preference":"bogus"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
		{"TypeUnknown rejected", `{"worker_preference":"unknown"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
		{"case sensitive rejected", `{"worker_preference":"Claude_Code"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := env.patchWorkspace(t, cookie, ws.ID, tt.body)
			require.Equal(t, tt.wantStatus, rec.Code, "body=%s resp=%s", tt.body, rec.Body.String())
			if tt.wantCode != "" {
				require.Contains(t, rec.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestPATCHWorkspaceWorkerPreference_Persists(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.createUserAndLogin(t, "owner", true)
	ws := env.createWorkspace(t, cookie, "owner")

	// Set a valid preference
	rec := env.patchWorkspace(t, cookie, ws.ID, `{"worker_preference":"acp"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	// Re-fetch and confirm persisted
	got := env.getWorkspace(t, cookie, ws.ID)
	require.Equal(t, "acp", got.WorkerPreference)
}
```

> **Note:** 确认 `newTestAuthEnv` / `createUserAndLogin` / `createWorkspace` / `getWorkspace` helper 名称与 spec ① ② 测试一致（若 `getWorkspace` 不存在，用 `GET /api/workspaces/{id}` 直接 patchWorkspace 风格写）。参考 spec ② `TestPATCHWorkspaceAgentConfigOverrides_Persists`（workspace_handlers_test.go:216 区域）的现有模式对齐。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestPATCHWorkspaceWorkerPreference' ./internal/gateway/... -count=1`
Expected: FAIL — 非法 worker_preference 当前被原样存储（:179-181 无校验），`unknown`/`bogus` 用例返回 200 而非 400。

- [ ] **Step 3: Write minimal implementation**

修改 `internal/gateway/workspace_handlers.go:179-181`：

```go
	if req.WorkerPreference != "" {
		if err := worker.ValidateType(worker.WorkerType(req.WorkerPreference)); err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_WORKER_TYPE", err.Error())
			return
		}
		ws.WorkerPreference = req.WorkerPreference
	}
```

确认 `workspace_handlers.go` 顶部已 import `"github.com/hrygo/hotplex/internal/worker"`（spec ① 应已 import；若无需补）。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run 'TestPATCHWorkspaceWorkerPreference' ./internal/gateway/... -count=1 -race`
Expected: PASS（含 8 白名单子测试 + Persists）

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/workspace_handlers.go internal/gateway/workspace_handlers_test.go
git commit -m "feat(gateway): PATCH worker_preference whitelist (spec ③)"
```

---

## Task 3: CreateSession worker_type 白名单校验（TDD）

**Files:**
- Modify: `internal/gateway/api.go:266-276`
- Test: `internal/gateway/api_test.go`（复用现有 CreateSession 测试模式，如 `TestCreateSession_WithClientSessionID` @252）

- [ ] **Step 1: Write the failing test**

追加到 `internal/gateway/api_test.go`（CreateSession 测试组附近）。复用现有 `newTestAuthEnv` / workspace 创建 helper：

```go
func TestCreateSession_InvalidWorkerType(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.createUserAndLogin(t, "owner", true)
	ws := env.createWorkspace(t, cookie, "owner")

	tests := []struct {
		name       string
		queryParam string // ?worker_type=
		wantStatus int
	}{
		{"query bogus rejected", "bogus", http.StatusBadRequest},
		{"query TypeUnknown rejected", "unknown", http.StatusBadRequest},
		{"query case sensitive rejected", "Claude_Code", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// 用 query param 形式（api.go:269 路径），body 无 worker_type
			rec := env.createSessionRaw(t, cookie, ws.ID, "", "client-key-"+tt.name, "GET", "?worker_type="+tt.queryParam)
			require.Equal(t, tt.wantStatus, rec.Code, "resp=%s", rec.Body.String())
			require.Contains(t, rec.Body.String(), "INVALID_WORKER_TYPE")
		})
	}

	t.Run("valid query worker_type accepted", func(t *testing.T) {
		t.Parallel()
		rec := env.createSessionRaw(t, cookie, ws.ID, "", "client-key-valid", "GET", "?worker_type=acp")
		// 200 或经 bridge（可能 bridge error 但非 400 INVALID_WORKER_TYPE）
		require.NotContains(t, rec.Body.String(), "INVALID_WORKER_TYPE")
	})
}
```

> **Note:** `createSessionRaw` 是占位名——核对现有 CreateSession 测试（api_test.go:252-369）用的 helper 签名（可能是 `env.createSession(t, cookie, ws.ID, ...)` 或直接 `httptest` 调 `/api/sessions`）。用现有 helper 的真实签名替换 `createSessionRaw`，保持传 `(workspaceID, body, clientSessionID, method, query)` 的语义。关键断言：非法 worker_type → 400 + `INVALID_WORKER_TYPE`。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestCreateSession_InvalidWorkerType ./internal/gateway/... -count=1`
Expected: FAIL — 非法 query `worker_type` 当前无校验，会落到 fallback / launch 失败（非 400 `INVALID_WORKER_TYPE`）。

- [ ] **Step 3: Write minimal implementation**

修改 `internal/gateway/api.go:266-276`，在 fallback 到 preference/default 前校验解析出的非空 wt：

```go
	// worker_type resolution: body/query > workspace.WorkerPreference > default.
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
	if wt == "" {
		wt = worker.WorkerType(ws.WorkerPreference)
	}
	if wt == "" {
		wt = worker.TypeClaudeCode
	}
```

> **关键：** 校验只对 body/query 解析出的 wt（请求侧，可能非法）。`ws.WorkerPreference`（DB）已在 PATCH 写入侧校验（Task 2），此处不再重复校验——若 PATCH 闭环，DB 值必然合法。`TypeClaudeCode` default 是常量，无需校验。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run 'TestCreateSession' ./internal/gateway/... -count=1 -race`
Expected: PASS（新 `TestCreateSession_InvalidWorkerType` + 现有 CreateSession 全回归通过）

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/api.go internal/gateway/api_test.go
git commit -m "feat(gateway): CreateSession worker_type whitelist (spec ③)"
```

---

## Task 4: 双轨隔离 + 会话连续性回归测试

**Files:**
- Test: `internal/session/key_test.go`（DeriveSessionKey 切 worker_type = 新 key，已部分有 @22 `TestDeriveSessionKey_DifferentTuples`，补明确断言）
- Test: `internal/gateway/api_test.go`（双轨隔离：Message Channel 轨 worker_type 来自 config，不受 WebChat preference 影响——文档化回归）

- [ ] **Step 1: Write the failing/clarifying test**

追加到 `internal/session/key_test.go`（`TestDeriveSessionKey_DifferentTuples` 后）——**会话连续性**：切 worker_type 产生新 key，旧 key 不变：

```go
func TestDeriveSessionKey_SwitchWorkerType_NewKey(t *testing.T) {
	t.Parallel()
	// 会话连续性 (spec ③ §8): 切 worker_preference → DeriveSessionKey 新 wt → 新 session key → 新会话。
	// 已存在会话（旧 key）不受影响。
	keyCC := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1", "ws-1", "/tmp/hotplex/ws")
	keyOCS := DeriveSessionKey("u1", worker.TypeOpenCodeSrv, "s1", "ws-1", "/tmp/hotplex/ws")
	require.NotEqual(t, keyCC, keyOCS,
		"switching worker_type must produce a new session key (new session), not resume the old one")
}
```

> **Note:** `key_test.go` 已 import `worker`（@15 用 `worker.TypeClaudeCode`）。无需补 import。

- [ ] **Step 2: Run test — expect PASS already（这是回归/文档化测试，验证现有不变式）**

Run: `go test -run TestDeriveSessionKey_SwitchWorkerType_NewKey ./internal/session/... -count=1 -race`
Expected: PASS（DeriveSessionKey 含 worker_type，切则不同——现有行为，此测试锁定它防回归）。

> 双轨隔离（Message Channel 轨 worker_type 来自 `config_defaults.propagatePlatform`，不查 `worker_preference`）是**结构性保证**——Message Channel 会话不绑 workspace（spec ① §2.4），代码路径无交集。无需新测试代码，但在 Task 5 的 design spec/CLAUDE.md 文档化此不变式。若需显式回归，在 `config_defaults_test.go` 加：`propagatePlatform` 不引用 `Workspace.WorkerPreference`（grep 确认无引用即可，作为轻量文档化）。

- [ ] **Step 3: Commit**

```bash
git add internal/session/key_test.go
git commit -m "test(session): DeriveSessionKey switch-worker-type new-key regression (spec ③ §8)"
```

---

## Task 5: 文档化（design spec 双轨 + 会话连续性已在；补 .agents/rules worker-proc.md / CLAUDE.md 一行引用）

**Files:**
- Modify: `docs/specs/WebChat-Multitenancy-WorkspaceWorker-Design-Spec.md`（已含 §6 双轨隔离 + §8 会话连续性，确认无需改）
- 可选：`.agents/rules/golang.md` 或 worker 规则补「worker_type 边界校验用 `worker.ValidateType`」

- [ ] **Step 1: 确认 design spec §6/§8 已覆盖**

Read `docs/specs/WebChat-Multitenancy-WorkspaceWorker-Design-Spec.md` §6（双轨隔离）+ §8（会话连续性）—— 已在 brainstorming 写入。无需改。

- [ ] **Step 2: 若 .agents/rules 有 worker 选择相关规则，补 ValidateType 引用**

Grep: `grep -rn "worker_type\|WorkerPreference" .agents/rules/`
若命中 `golang.md` 或 `session.md` 的 worker 选择段落，补一行：「worker_type 边界校验：PATCH workspace.worker_preference + CreateSession body/query 统一走 `worker.ValidateType`（spec ③），非法 → 400 `INVALID_WORKER_TYPE`」。

若无命中，跳过此 Step（YAGNI）。

- [ ] **Step 3: 全量质量门禁**

Run: `make check`
Expected: PASS（quality + build + test，三平台逻辑）

- [ ] **Step 4: Commit（若有规则改动）**

```bash
git add .agents/rules/  # 仅当 Step 2 改了
git commit -m "docs(rules): worker_type boundary validation (spec ③)"
```

---

## Self-Review

**1. Spec coverage:**
- §4 ValidateType → Task 1 ✓
- §7.1 PATCH 校验 → Task 2 ✓
- §7.2 CreateSession 校验 → Task 3 ✓
- §8 会话连续性 → Task 4（key_test 回归）+ design spec §8 ✓
- §6 双轨隔离 → Task 4 Step 2（结构性保证 + design spec §6）+ Task 5 ✓
- §10 测试策略（ValidateType / PATCH / CreateSession / 双轨 / 切 preference）→ Task 1-4 全覆盖 ✓
- §11 受影响文件 → registry.go / registry_test.go / workspace_handlers.go / workspace_handlers_test.go / api.go / api_test.go / key_test.go 全在 Task ✓

**2. Placeholder scan:** 无 TBD/TODO。"createSessionRaw"/"getWorkspace" 标注为「核对现有 helper 签名替换」——是 plan 内明确指引（非占位），实现者按 spec ② 现有 helper 对齐。✓

**3. Type consistency:**
- `ValidateType(wt WorkerType) error` — Task 1 定义，Task 2/3 调用一致 ✓
- `ErrInvalidWorkerType` — Task 1 定义，Task 1 测试 `errors.Is` 用 ✓
- `INVALID_WORKER_TYPE` code — Task 2/3 一致 ✓
- `worker.WorkerType(req.WorkerPreference)` / `worker.WorkerType(body.WorkerType)` — 调用一致 ✓

无问题，plan 完整。
