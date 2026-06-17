# WebChat 多租户 spec ②（per-workspace agent-configs）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 WebChat 轨的 agent-configs（SOUL/AGENTS/SKILLS/USER/MEMORY）支持 per-workspace 两层继承（团队默认 → workspace 自定义），同一团队不同 workspace 各自定制，互不影响；Message Channel 轨行为零变化。

**Architecture:** `agentconfig` 包新增 `LoadForWorkspace`（复用 `Load` 取团队默认，再逐文件覆盖）+ `ValidateOverrides`（JSON/键/类型/size 校验）。`gateway` 包给 `Bridge` 加窄接口 `WorkspaceOverridesReader` 依赖 + `resolveWorkspaceOverrides` helper，在 3 个 worker 启动调用点（StartSession / resume / fresh-start）统一解析 workspace overrides；`workerLaunchParams` 携带已解析 map，`injectAgentConfig` 纯函数分流（有 overrides → `LoadForWorkspace`，无 → `Load`）。`workspace_handlers` PATCH 补三层校验。复用 spec ① `agent_config_overrides` 列，无新迁移。

**Tech Stack:** Go 1.22+、`encoding/json`、`testify/require`（table-driven + `t.Parallel()`）、`httptest`。

**关联设计:** [`../specs/WebChat-Multitenancy-PerWorkspace-AgentConfigs-Design-Spec.md`](../specs/WebChat-Multitenancy-PerWorkspace-AgentConfigs-Design-Spec.md)

**验收方式:** Go 单元 + 集成测试（`-count=1 -race`，单模块 ≤5s）。核心：两 workspace 不同 overrides → 各自 session 注入不同 system prompt；`Load` 与 Message Channel 轨行为零变化。

---

## 关键设计决策（执行时不可偏离）

| 决策 | 值 | 来源 |
|---|---|---|
| 存储形式 | DB JSON flat map（`agent_config_overrides` 列，spec ① 已建） | spec §3/§4 |
| 继承语义 | 逐文件覆盖（命中即终止，与 `resolveFile` 一致） | spec §3/§5 |
| 可覆盖范围 | 全部 5 文件；`META-COGNITION.md` 不在白名单，物理不可覆盖 | spec §3/§9 |
| 解析入口 | 新增 `LoadForWorkspace` 独立函数；`Load` 零改动 | spec §3/§6 |
| 数据流 | Bridge 加窄接口 `WSStore` + helper；`workerLaunchParams` 携已解析 map；`injectAgentConfig` 纯函数 | spec §7 |
| 清除语义 | `"{}"` = 清除覆盖；`""`/省略 = 不更新（与 spec ① PATCH 一致） | spec §8.3 |
| 未知键 | PATCH 拒绝（400）；`LoadForWorkspace` 忽略（纵深防御） | spec §5/§8 |

## 文件结构

| 文件 | 责任 | 改动 |
|---|---|---|
| `internal/agentconfig/validate.go` | `ValidateOverrides` + sentinel errors | 新建 |
| `internal/agentconfig/validate_test.go` | 校验单测 | 新建 |
| `internal/agentconfig/loader.go` | `LoadForWorkspace` + `applyOverrides` | 修改 |
| `internal/agentconfig/loader_test.go` | `LoadForWorkspace` 单测 + `Load` 回归 | 扩展 |
| `internal/gateway/deps.go` | `WorkspaceOverridesReader` 接口 + `BridgeDeps.WSStore` | 修改 |
| `internal/gateway/bridge.go` | `Bridge.wsStore` 字段 + NewBridge 注入 + 2 调用点接线 | 修改 |
| `internal/gateway/bridge_worker.go` | `resolveWorkspaceOverrides` helper + `workerLaunchParams` 字段 + `injectAgentConfig` 分流 + 2 调用点接线 | 修改 |
| `internal/gateway/bridge_worker_test.go` | helper 单测（mock WSStore） | 新建 |
| `cmd/hotplex/gateway_run.go` | NewBridge（:260）传 `WSStore` | 修改 |
| `internal/gateway/workspace_handlers.go` | PATCH 三层校验 | 修改 |
| `internal/gateway/workspace_handlers_test.go` | PATCH 校验测试 | 扩展 |

## 依赖方向

```
cmd/hotplex ──▶ gateway ──▶ agentconfig   (单向，已有)
```

`agentconfig` 包不依赖 `gateway`/`session`（`ValidateOverrides`/`LoadForWorkspace` 是纯函数）。`gateway` 依赖 `agentconfig`（已存在）+ `session`（`WorkspaceOverridesReader` 返回 `*session.Workspace`，已存在）。

## 测试规范（所有任务通用）

- table-driven + `testify/require` + `t.Parallel()`
- 禁 `time.Sleep`；单模块 ≤5s（`-count=1 -race`）
- 临时目录用 `t.TempDir()`；文件写入用现有 `writeFile(t, dir, name, content)` helper（见 `loader_test.go`）

---

## Task 1: `ValidateOverrides` + sentinel errors（agentconfig 包）

**Files:**
- Create: `internal/agentconfig/validate.go`
- Test: `internal/agentconfig/validate_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentconfig/validate_test.go`：

```go
package agentconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr error // sentinel; nil expects success
		want    map[string]string
	}{
		{
			name: "empty raw returns nil map",
			raw:  "",
			want: nil,
		},
		{
			name: "valid flat map with subset of files",
			raw:  `{"SOUL.md":"persona","USER.md":"profile"}`,
			want: map[string]string{"SOUL.md": "persona", "USER.md": "profile"},
		},
		{
			name:    "invalid JSON",
			raw:     `{not json`,
			wantErr: ErrInvalidConfigJSON,
		},
		{
			name:    "unknown key rejected",
			raw:     `{"META-COGNITION.md":"x"}`,
			wantErr: ErrUnknownConfigFile,
		},
		{
			name:    "unknown arbitrary key rejected",
			raw:     `{"foo.md":"x"}`,
			wantErr: ErrUnknownConfigFile,
		},
		{
			name:    "non-string value rejected",
			raw:     `{"SOUL.md":123}`,
			wantErr: ErrInvalidConfigValue,
		},
		{
			name:    "single file exceeds MaxFileChars",
			raw:     `{"SOUL.md":"` + strings.Repeat("a", MaxFileChars+1) + `"}`,
			wantErr: ErrConfigTooLarge,
		},
		{
			name:    "total exceeds MaxTotalChars",
			raw:     `{"SOUL.md":"` + strings.Repeat("a", MaxTotalChars+1) + `"}`,
			wantErr: ErrConfigTooLarge,
		},
		{
			name: "empty object clears overrides",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name: "all five known files accepted",
			raw:  `{"SOUL.md":"s","AGENTS.md":"a","SKILLS.md":"k","USER.md":"u","MEMORY.md":"m"}`,
			want: map[string]string{"SOUL.md": "s", "AGENTS.md": "a", "SKILLS.md": "k", "USER.md": "u", "MEMORY.md": "m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateOverrides(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/agentconfig/ -run TestValidateOverrides -count=1`
Expected: 编译失败 / FAIL（`ValidateOverrides` 及 sentinel errors 未定义）。

- [ ] **Step 3: 实现 `ValidateOverrides`**

创建 `internal/agentconfig/validate.go`：

```go
package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel errors for agent-config override validation (spec ② §10).
var (
	ErrInvalidConfigJSON  = errors.New("agentconfig: invalid config JSON")
	ErrUnknownConfigFile  = errors.New("agentconfig: unknown config file")
	ErrConfigTooLarge     = errors.New("agentconfig: config exceeds size limit")
	ErrInvalidConfigValue = errors.New("agentconfig: invalid config value")
)

// ValidateOverrides parses raw JSON into a flat map of config-file-name → content
// and validates keys (against configFiles), value types (must be string), and size
// limits (MaxFileChars per file, MaxTotalChars total). Returns the parsed map on
// success. Empty raw ("") returns (nil, nil) meaning "no overrides".
//
// Used by the workspace PATCH handler (write-time validation) and Bridge's
// resolveWorkspaceOverrides (read-time parsing). META-COGNITION.md is not in
// configFiles, so it is rejected here — workspace overrides cannot touch it.
func ValidateOverrides(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfigJSON, err)
	}
	known := make(map[string]struct{}, len(configFiles))
	for _, f := range configFiles {
		known[f] = struct{}{}
	}
	out := make(map[string]string, len(m))
	var total int
	for k, v := range m {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownConfigFile, k)
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %q must be a string", ErrInvalidConfigValue, k)
		}
		if len(s) > MaxFileChars {
			return nil, fmt.Errorf("%w: %q is %d chars, max %d", ErrConfigTooLarge, k, len(s), MaxFileChars)
		}
		total += len(s)
		out[k] = s
	}
	if total > MaxTotalChars {
		return nil, fmt.Errorf("%w: total %d chars exceeds max %d", ErrConfigTooLarge, total, MaxTotalChars)
	}
	return out, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/agentconfig/ -run TestValidateOverrides -count=1 -race`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/agentconfig/validate.go internal/agentconfig/validate_test.go
git commit -m "feat(agentconfig): ValidateOverrides for per-workspace config (spec ②)"
```

---

## Task 2: `LoadForWorkspace` + `applyOverrides`（agentconfig 包）

**Files:**
- Modify: `internal/agentconfig/loader.go`（追加函数）
- Test: `internal/agentconfig/loader_test.go`（追加测试）

- [ ] **Step 1: 写失败测试**

追加到 `internal/agentconfig/loader_test.go` 末尾：

```go
func TestLoadForWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("nil overrides inherits team defaults", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")
		writeFile(t, dir, "AGENTS.md", "team-rules")

		cfg, err := LoadForWorkspace(dir, "webchat", nil)
		require.NoError(t, err)
		require.Equal(t, "team-soul", cfg.Soul)
		require.Equal(t, "team-rules", cfg.Agents)
	})

	t.Run("override replaces team default per-file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")
		writeFile(t, dir, "AGENTS.md", "team-rules")

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)         // overridden
		require.Equal(t, "team-rules", cfg.Agents)    // inherited
	})

	t.Run("empty override value inherits team default", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")

		overrides := map[string]string{"SOUL.md": ""}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "team-soul", cfg.Soul) // empty value does not override
	})

	t.Run("override without team default applies", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no team files

		overrides := map[string]string{"USER.md": "ws-user"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-user", cfg.User)
	})

	t.Run("injectExclude wins over override", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides, "SOUL.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul) // excluded even though overridden
	})

	t.Run("unknown override keys ignored (defense-in-depth)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		overrides := map[string]string{"META-COGNITION.md": "evil", "foo.md": "x", "SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)
		// unknown keys silently ignored; META-COGNITION never appears in AgentConfigs
	})

	t.Run("platform-level team default resolves before override", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "webchat/AGENTS.md", "webchat-team-rules") // platform-level

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)                  // override
		require.Equal(t, "webchat-team-rules", cfg.Agents)     // platform-level team default
	})
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/agentconfig/ -run TestLoadForWorkspace -count=1`
Expected: 编译失败（`LoadForWorkspace` 未定义）。

- [ ] **Step 3: 实现 `LoadForWorkspace` + `applyOverrides`**

在 `internal/agentconfig/loader.go` 末尾（`IsEmpty` 方法后）追加：

```go
// LoadForWorkspace resolves WebChat-track agent configs via two-level inheritance:
// team defaults (loaded from dir via Load with botName="") → workspace overrides.
//
// Each non-empty override entry replaces the corresponding team-default field.
// injectExclude has highest priority: an excluded file is never injected even if
// overridden. Unknown override keys are silently ignored (defense-in-depth —
// ValidateOverrides rejects them at write time).
//
// The Message Channel track calls Load directly; this function is WebChat-only.
// See design spec §5.
func LoadForWorkspace(dir, platform string, overrides map[string]string, injectExclude ...string) (*AgentConfigs, error) {
	base, err := Load(dir, platform, "", injectExclude...)
	if err != nil {
		return nil, err
	}
	applyOverrides(base, overrides, injectExclude)
	return base, nil
}

// applyOverrides applies per-file overrides onto base in place. Only keys in
// configFiles are applied; empty values do not override; excluded files are skipped.
func applyOverrides(base *AgentConfigs, overrides map[string]string, injectExclude []string) {
	set := func(baseName, val string, target *string) {
		if val == "" || shouldExclude(baseName, injectExclude) {
			return
		}
		*target = val
	}
	for k, v := range overrides {
		switch k {
		case "SOUL.md":
			set(k, v, &base.Soul)
		case "AGENTS.md":
			set(k, v, &base.Agents)
		case "SKILLS.md":
			set(k, v, &base.Skills)
		case "USER.md":
			set(k, v, &base.User)
		case "MEMORY.md":
			set(k, v, &base.Memory)
		}
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/agentconfig/ -count=1 -race`
Expected: PASS（含 `TestLoadForWorkspace` 全部子测试 + `TestLoad` 等现有测试回归通过——`Load` 行为零变化）。

- [ ] **Step 5: 提交**

```bash
git add internal/agentconfig/loader.go internal/agentconfig/loader_test.go
git commit -m "feat(agentconfig): LoadForWorkspace two-level inheritance (spec ②)"
```

---

## Task 3: `WorkspaceOverridesReader` 接口 + `BridgeDeps.WSStore`（gateway 包）

**Files:**
- Modify: `internal/gateway/deps.go`（`BridgeDeps` 加字段 + 接口定义）
- Modify: `internal/gateway/bridge.go`（`Bridge.wsStore` 字段 + NewBridge 注入）
- Test: `internal/gateway/bridge_worker_test.go`（新建，helper 单测；Task 4 加 helper 本体）

> 本 task 只加接口 + 字段 + 注入接线，helper 本体在 Task 4（与 injectAgentConfig 分流一起，因 helper 与 injectAgentConfig 都在 bridge_worker.go，紧邻）。本 task 的测试在 Task 4 完成后才能跑通——故 Task 3 步骤 4 改为编译验证，Task 4 步骤 4 一起跑 helper 测试。

- [ ] **Step 1: 加接口 + BridgeDeps 字段**

在 `internal/gateway/deps.go` 顶部 import 区确认有 `context` 与 `session`（若无可参考同文件其他类型引用；该文件已为 gateway 包内部，`session` 通常已引用）。在 `BridgeDeps` struct 末尾（`AgentConfigExclude` 字段后）追加字段，并在文件合适位置定义接口：

```go
// WorkspaceOverridesReader is the narrow workspace-store subset Bridge needs to
// resolve per-workspace agent-config overrides (spec ② §7.3). Kept separate from
// session.UserWorkspaceStore so tests mock a single method.
type WorkspaceOverridesReader interface {
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
}
```

`BridgeDeps` 末尾追加：

```go
	WSStore WorkspaceOverridesReader // WebChat per-workspace agent-config overrides (spec ②); nil = disabled
```

若 `deps.go` 未 import `context`/`session`，补 import。若不确定，运行 `go build ./internal/gateway/` 按报错补。

- [ ] **Step 2: Bridge struct 加字段 + NewBridge 注入**

在 `internal/gateway/bridge.go` 的 `Bridge` struct（:42 起）中，`agentConfigDir` 字段附近追加：

```go
	wsStore            WorkspaceOverridesReader // per-workspace agent-config overrides resolver (spec ②); nil = Message Channel track
```

在 `NewBridge`（:96 起）的 `&Bridge{...}` 初始化块中（`agentConfigDir: deps.AgentConfigDir,` 行附近）追加：

```go
		wsStore:            deps.WSStore,
```

- [ ] **Step 3: 编译验证**

Run: `go build ./internal/gateway/ ./cmd/hotplex/`
Expected: PASS。`BridgeDeps.WSStore` 与 `Bridge.wsStore` 字段新增但本 task 未被读取（helper 在 Task 4）——Go struct 字段允许未被使用，不报错。`cmd/hotplex` 的 NewBridge 调用尚未传 `WSStore`，零值为 nil，编译通过（`BridgeDeps` 字段在字面量中可省略）。

- [ ] **Step 4: 暂不提交**（与 Task 4 合并提交，保持每次提交可编译可测试）

---

## Task 4: `resolveWorkspaceOverrides` helper + `workerLaunchParams` + `injectAgentConfig` 分流 + 调用点接线

**Files:**
- Modify: `internal/gateway/bridge_worker.go`（helper + workerLaunchParams + injectAgentConfig + :82 + fresh-start :213）
- Modify: `internal/gateway/bridge.go`（StartSession :189 + resume :297 调用点）
- Test: `internal/gateway/bridge_worker_test.go`（新建）

- [ ] **Step 1: 写 helper 失败测试**

创建 `internal/gateway/bridge_worker_test.go`（`testLogger` 在本文件内定义；若 gateway 包已有同名 helper，执行时将其删除并复用现有那个）：

```go
package gateway

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// testLogger returns a minimal logger for bridge unit tests. If the gateway
// package already defines a *slog.Logger test helper, delete this and reuse it.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// stubWSStore implements WorkspaceOverridesReader for bridge helper tests.
type stubWSStore struct {
	ws  *session.Workspace
	err error
}

func (s *stubWSStore) GetWorkspaceByID(_ context.Context, _ string) (*session.Workspace, error) {
	return s.ws, s.err
}

// newBridgeForOverrideTest builds a minimal Bridge with only wsStore + log set,
// enough to exercise resolveWorkspaceOverrides without the full dependency graph.
func newBridgeForOverrideTest(t *testing.T, ws *session.Workspace, err error) *Bridge {
	t.Helper()
	return &Bridge{
		log:     testLogger(t),
		wsStore: &stubWSStore{ws: ws, err: err},
	}
}

func TestResolveWorkspaceOverrides(t *testing.T) {
	t.Parallel()

	t.Run("empty workspace id returns nil", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, nil, nil)
		require.Nil(t, b.resolveWorkspaceOverrides(""))
	})

	t.Run("nil wsStore returns nil", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{log: testLogger(t)} // wsStore zero-value nil
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("valid overrides parsed", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{"SOUL.md":"x","USER.md":"y"}`}
		b := newBridgeForOverrideTest(t, ws, nil)
		got := b.resolveWorkspaceOverrides("ws-1")
		require.Equal(t, map[string]string{"SOUL.md": "x", "USER.md": "y"}, got)
	})

	t.Run("empty overrides string returns nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: ""}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("store error degrades to nil", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, nil, errors.New("boom"))
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("invalid JSON degrades to nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{bad`}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})
}

// testLogger returns a minimal slog.Logger for bridge tests. If gateway package
// already has a test helper returning *slog.Logger, prefer it; otherwise use slog.Default.
// (If a name clash occurs, rename this helper.)
```

> **注意 `testLogger`：** 若 gateway 包测试已有返回 `*slog.Logger` 的 helper（grep `func.*Logger.*testing.T` in `internal/gateway/*_test.go`），复用它并删除本文件中的 `testLogger`；否则在本文件追加：
> ```go
> import "log/slog"
> func testLogger(t *testing.T) *slog.Logger { t.Helper(); return slog.Default() }
> ```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/gateway/ -run TestResolveWorkspaceOverrides -count=1`
Expected: 编译失败（`resolveWorkspaceOverrides` 未定义）。

- [ ] **Step 3: 实现 helper + workerLaunchParams + injectAgentConfig 分流**

**3a.** 在 `internal/gateway/bridge_worker.go` 的 `workerLaunchParams` struct（:31 起）末尾追加字段：

```go
	workspaceOverrides map[string]string // WebChat per-workspace config overrides (spec ②); nil = Message Channel track → Load
```

**3b.** 在 `bridge_worker.go` 中 `injectAgentConfig` 上方追加 helper：

```go
// resolveWorkspaceOverrides fetches a workspace's agent-config overrides and parses
// them. Returns nil for empty workspaceID (Message Channel track) or nil wsStore, and
// degrades to nil (team defaults) on any fetch/parse error — never blocks worker launch.
// See design spec §7.3.
func (b *Bridge) resolveWorkspaceOverrides(workspaceID string) map[string]string {
	if workspaceID == "" || b.wsStore == nil {
		return nil
	}
	ws, err := b.wsStore.GetWorkspaceByID(context.Background(), workspaceID)
	if err != nil {
		b.log.Warn("bridge: fetch workspace overrides failed, degrading to team defaults",
			"workspace_id", workspaceID, "err", err)
		return nil
	}
	overrides, err := agentconfig.ValidateOverrides(ws.AgentConfigOverrides)
	if err != nil {
		b.log.Warn("bridge: parse workspace overrides failed, degrading to team defaults",
			"workspace_id", workspaceID, "err", err)
		return nil
	}
	return overrides
}
```

**3c.** 改 `injectAgentConfig` 签名（:328）加 `workspaceOverrides map[string]string` 末参，并在 `agentconfig.Load` 调用处分流。当前 :341 一行：

```go
	configs, err := agentconfig.Load(b.agentConfigDir, platform, botName, injectExclude...)
```

替换为：

```go
	var configs *agentconfig.AgentConfigs
	if workspaceOverrides != nil {
		configs, err = agentconfig.LoadForWorkspace(b.agentConfigDir, platform, workspaceOverrides, injectExclude...)
	} else {
		configs, err = agentconfig.Load(b.agentConfigDir, platform, botName, injectExclude...)
	}
```

并修改函数签名行（:328）：

```go
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botName, botID string, injectExclude []string, workspaceOverrides map[string]string) {
```

> 注意：`botName` 在 `workspaceOverrides != nil` 分支不再传给 Load（LoadForWorkspace 内部用 `botName=""`）。这正确——WebChat 会话不选 Bot（spec ① §2.4）。

**3d.** 改 `createAndLaunchWorker`（:82）透传：

当前 :82：
```go
	b.injectAgentConfig(&params.workerInfo, params.platform, params.botName, params.botID, params.injectExclude)
```
改为：
```go
	b.injectAgentConfig(&params.workerInfo, params.platform, params.botName, params.botID, params.injectExclude, params.workspaceOverrides)
```

- [ ] **Step 4: 接线 3 个调用点填 workspaceOverrides**

**4a.** `internal/gateway/bridge.go` StartSession（:189 起 `workerLaunchParams{...}`）末尾（`injectExclude: p.InjectExclude,` 后）追加：

```go
		workspaceOverrides: b.resolveWorkspaceOverrides(p.WorkspaceID),
```

**4b.** `internal/gateway/bridge.go` resume（:297 起 `workerLaunchParams{...}`）末尾（`forwardOpts: &opts,` 后）追加：

```go
		workspaceOverrides: b.resolveWorkspaceOverrides(si.WorkspaceID),
```

**4c.** `internal/gateway/bridge_worker.go` fresh-start（:213 起 `workerLaunchParams{...}`）末尾（`injectExclude: nil,` 后）追加：

```go
		workspaceOverrides: b.resolveWorkspaceOverrides(si.WorkspaceID),
```

- [ ] **Step 5: 运行 helper 测试 + 全 gateway 编译**

Run: `go test ./internal/gateway/ -run TestResolveWorkspaceOverrides -count=1 -race`
Expected: PASS。

Run: `go build ./internal/gateway/`
Expected: PASS（3 调用点接线后编译通过）。

- [ ] **Step 6: 回归现有 gateway 测试**

Run: `go test ./internal/gateway/ -count=1 -race -short`
Expected: PASS（Message Channel 轨行为零变化；workspaceOverrides 在非 WebChat 会话为 nil → 走原 Load 路径）。

- [ ] **Step 7: 提交（Task 3 + Task 4 合并）**

```bash
git add internal/gateway/deps.go internal/gateway/bridge.go internal/gateway/bridge_worker.go internal/gateway/bridge_worker_test.go
git commit -m "feat(gateway): bridge resolves per-workspace agent-config overrides (spec ②)

BridgeDeps 加窄接口 WSStore + resolveWorkspaceOverrides helper；3 个 worker
启动调用点（StartSession/resume/fresh-start）统一解析 workspace overrides；
workerLaunchParams 携已解析 map；injectAgentConfig 纯函数分流 LoadForWorkspace/Load。"
```

---

## Task 5: NewBridge 调用点传 `WSStore`（cmd/hotplex）

**Files:**
- Modify: `cmd/hotplex/gateway_run.go:260`（NewBridge 调用）

- [ ] **Step 1: 传入 WSStore**

在 `cmd/hotplex/gateway_run.go` 的 `gateway.NewBridge(gateway.BridgeDeps{...})` 调用块（:260 起）末尾（`AgentConfigExclude: buildAgentConfigExclude(cfg),` 后）追加：

```go
		WSStore:            deps.WorkspaceStore,
```

> `deps.WorkspaceStore` 已存在（`gateway_run.go:78` `GatewayDeps.WorkspaceStore session.UserWorkspaceStore`），且 `session.UserWorkspaceStore` 含 `GetWorkspaceByID`，满足 `WorkspaceOverridesReader` 接口（结构化类型匹配，无需显式适配）。

- [ ] **Step 2: 编译验证全项目**

Run: `go build ./...`
Expected: PASS。

- [ ] **Step 3: 提交**

```bash
git add cmd/hotplex/gateway_run.go
git commit -m "feat(cmd): wire WorkspaceStore into Bridge for spec ② overrides"
```

---

## Task 6: PATCH 三层校验（gateway 包）

**Files:**
- Modify: `internal/gateway/workspace_handlers.go`（Update 方法 :162-164）
- Test: `internal/gateway/workspace_handlers_test.go`（追加）

- [ ] **Step 1: 写失败测试**

追加到 `internal/gateway/workspace_handlers_test.go`。先加一个 helper（若文件无类似 PATCH helper）：

```go
func (e *testAuthEnv) patchWorkspace(t *testing.T, cookie, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+id, bytes.NewReader([]byte(body)))
	req.SetPathValue("id", id)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	e.wsHandlers.Update(w, req)
	return w
}
```

追加测试函数：

```go
func TestWorkspace_PatchAgentConfigOverrides_Validation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-patch")

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "valid overrides accepted",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":\"x\",\"USER.md\":\"y\"}"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty object clears overrides",
			body:       `{"agent_config_overrides":"{}"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid JSON rejected",
			body:       `{"agent_config_overrides":"{not json"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_CONFIG_JSON",
		},
		{
			name:       "unknown key rejected",
			body:       `{"agent_config_overrides":"{\"META-COGNITION.md\":\"x\"}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "UNKNOWN_CONFIG_FILE",
		},
		{
			name:       "non-string value rejected",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":123}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_CONFIG_VALUE",
		},
		{
			name:       "oversized file rejected",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":\"" + strings.Repeat("a", 8001) + "\"}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "CONFIG_TOO_LARGE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.patchWorkspace(t, cookie, ws.ID, tt.body)
			require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			if tt.wantCode != "" {
				require.Contains(t, w.Body.String(), tt.wantCode)
			}
		})
	}
}

// TestWorkspace_PatchAgentConfigOverrides_Persists verifies a valid override is
// stored and round-trips through Get.
func TestWorkspace_PatchAgentConfigOverrides_Persists(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-persist")

	w := env.patchWorkspace(t, cookie, ws.ID, `{"agent_config_overrides":"{\"SOUL.md\":\"ws-soul\"}"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Get round-trips the stored JSON
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set("Cookie", cookie)
	gr := httptest.NewRecorder()
	env.wsHandlers.Get(gr, req)
	require.Equal(t, http.StatusOK, gr.Code)
	require.Contains(t, gr.Body.String(), `"SOUL.md":"ws-soul"`)
}
```

> **import：** 测试文件顶部补 `"strings"`（若未有）。

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/gateway/ -run TestWorkspace_PatchAgentConfigOverrides -count=1`
Expected: 部分 FAIL——当前 :162-164 无校验，非法 JSON / 未知键 / 超 size / 类型错误 均返回 200 而非 400。

- [ ] **Step 3: 实现三层校验**

在 `internal/gateway/workspace_handlers.go` 顶部 import 区加 `"github.com/hrygo/hotplex/internal/agentconfig"`（`errors` 已在文件 import 中）。

替换 `Update` 方法中 :162-164 三行：

```go
	if req.AgentConfigOverrides != "" {
		ws.AgentConfigOverrides = req.AgentConfigOverrides
	}
```

为：

```go
	if req.AgentConfigOverrides != "" {
		if _, err := agentconfig.ValidateOverrides(req.AgentConfigOverrides); err != nil {
			switch {
			case errors.Is(err, agentconfig.ErrInvalidConfigJSON):
				writeAppError(w, http.StatusBadRequest, "INVALID_CONFIG_JSON", err.Error())
			case errors.Is(err, agentconfig.ErrUnknownConfigFile):
				writeAppError(w, http.StatusBadRequest, "UNKNOWN_CONFIG_FILE", err.Error())
			case errors.Is(err, agentconfig.ErrConfigTooLarge):
				writeAppError(w, http.StatusBadRequest, "CONFIG_TOO_LARGE", err.Error())
			default:
				writeAppError(w, http.StatusBadRequest, "INVALID_CONFIG_VALUE", err.Error())
			}
			return
		}
		ws.AgentConfigOverrides = req.AgentConfigOverrides
	}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/gateway/ -run TestWorkspace_PatchAgentConfigOverrides -count=1 -race`
Expected: PASS（全部子测试）。

- [ ] **Step 5: 提交**

```bash
git add internal/gateway/workspace_handlers.go internal/gateway/workspace_handlers_test.go
git commit -m "feat(gateway): validate workspace agent_config_overrides on PATCH (spec ②)"
```

---

## Task 7: 端到端隔离集成测试（两 workspace 不同 overrides）

**Files:**
- Test: `internal/gateway/api_workspace_session_test.go`（扩展）或新建 `internal/gateway/bridge_overrides_integration_test.go`

> 此测试验证完整链路：workspace overrides → bridge helper → LoadForWorkspace → 不同 system prompt。因 `injectAgentConfig` 依赖 `agentConfigDir` 文件 + worker 启动，纯单测较重；采用「直接验证 LoadForWorkspace 与 helper 组合」的轻量集成。

- [ ] **Step 1: 写集成测试**

新建 `internal/gateway/bridge_overrides_integration_test.go`（复用 Task 4 `bridge_worker_test.go` 中定义的 `testLogger` 与 `stubWSStore`，同包无需重复定义）：

```go
package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/session"
)

// TestBridge_TwoWorkspaces_DifferentOverrides verifies the core spec ② invariant:
// two workspaces with different overrides produce different system prompts, and
// neither pollutes the other nor the team default.
func TestBridge_TwoWorkspaces_DifferentOverrides(t *testing.T) {
	t.Parallel()

	// Team defaults on disk (global level).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("team-rules"), 0o644))

	wsA := &session.Workspace{ID: "ws-a", AgentConfigOverrides: `{"SOUL.md":"persona-A"}`}
	wsB := &session.Workspace{ID: "ws-b", AgentConfigOverrides: `{"SOUL.md":"persona-B"}`}
	storeA := &stubWSStore{ws: wsA}
	storeB := &stubWSStore{ws: wsB}

	promptA := buildPromptFor(t, dir, storeA, "ws-a")
	promptB := buildPromptFor(t, dir, storeB, "ws-b")

	require.Contains(t, promptA, "persona-A")
	require.NotContains(t, promptA, "persona-B")
	require.Contains(t, promptB, "persona-B")
	require.NotContains(t, promptB, "persona-A")
	// Both inherit team default AGENTS.md
	require.Contains(t, promptA, "team-rules")
	require.Contains(t, promptB, "team-rules")
}

// TestBridge_WorkspaceWithoutOverrides_InheritsTeamDefault verifies a workspace
// with empty overrides falls fully back to team defaults.
func TestBridge_WorkspaceWithoutOverrides_InheritsTeamDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("team-soul"), 0o644))

	ws := &session.Workspace{ID: "ws-x", AgentConfigOverrides: ""}
	store := &stubWSStore{ws: ws}

	prompt := buildPromptFor(t, dir, store, "ws-x")
	require.Contains(t, prompt, "team-soul")
}

// buildPromptFor exercises the same data path as injectAgentConfig:
// resolveWorkspaceOverrides → LoadForWorkspace → BuildSystemPrompt.
func buildPromptFor(t *testing.T, dir string, store *stubWSStore, workspaceID string) string {
	t.Helper()
	b := &Bridge{log: testLogger(t), wsStore: store, agentConfigDir: dir}
	overrides := b.resolveWorkspaceOverrides(workspaceID)
	require.NotNil(t, overrides)
	cfg, err := agentconfig.LoadForWorkspace(dir, "webchat", overrides)
	require.NoError(t, err)
	return agentconfig.BuildSystemPrompt(cfg)
}
```

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/gateway/ -run TestBridge_TwoWorkspaces -count=1 -race` 和 `go test ./internal/gateway/ -run TestBridge_WorkspaceWithoutOverrides -count=1 -race`
Expected: PASS。

- [ ] **Step 3: 提交**

```bash
git add internal/gateway/bridge_overrides_integration_test.go
git commit -m "test(gateway): two-workspace agent-config isolation (spec ② e2e)"
```

---

## Task 8: 全量质量门禁

- [ ] **Step 1: 格式 + vet + mod**

Run: `make fmt && go vet ./... && go mod verify`
Expected: 无变更 / PASS。

- [ ] **Step 2: lint**

Run: `make lint`
Expected: PASS（关注 agentconfig/gateway 新代码）。

- [ ] **Step 3: 全量测试（含 race）**

Run: `make test`
Expected: PASS。重点确认：
- `./internal/agentconfig/` 全绿（`TestValidateOverrides` / `TestLoadForWorkspace` / `TestLoad` 回归）
- `./internal/gateway/` 全绿（`TestResolveWorkspaceOverrides` / `TestWorkspace_PatchAgentConfigOverrides*` / `TestBridge_TwoWorkspaces*` / 现有 workspace/auth/session 测试回归）

- [ ] **Step 4: 完整 CI 等价**

Run: `make check`
Expected: PASS。

- [ ] **Step 5: （可选）手动 curl 冒烟（需 gateway 运行）**

```bash
# 创建 workspace、登录 cookie 后：
curl -X PATCH localhost:PORT/api/workspaces/<id> \
  -H "Cookie: <cookie>" \
  -d '{"agent_config_overrides":"{\"SOUL.md\":\"test-persona\"}"}'
# 期望 200；再用该 workspace 创建 session，观察日志 "agent config injected" + prompt 含 test-persona
```

> 若无运行环境，跳过此步——Task 1-7 的自动化测试已覆盖正确性。

---

## 完成标准

- [ ] `ValidateOverrides` + `LoadForWorkspace` 实现并通过单测
- [ ] `Load` 与 Message Channel 轨现有测试零回归（双轨隔离证据）
- [ ] Bridge 3 个 worker 启动调用点统一经 `resolveWorkspaceOverrides` 解析 overrides
- [ ] `injectAgentConfig` 按 `workspaceOverrides` 分流，纯函数
- [ ] `cmd/hotplex` 注入 `WSStore`，全项目编译通过
- [ ] PATCH `agent_config_overrides` 三层校验（JSON/键/类型/size）
- [ ] 两 workspace 不同 overrides 端到端隔离测试通过
- [ ] `make check` 全绿
- [ ] 每个任务独立 commit，提交信息符合项目规范
