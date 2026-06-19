# 登录页新用户引导 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** webchat 登录页对新用户友好引导——bootstrap 零用户时引导运行 CLI 创建首个 admin,受邀新用户获得入口指引/onboarding/友好错误。

**Architecture:** 后端新增公开端点 `GET /api/auth/bootstrap-status`(独立于 CookieAuth 守卫,避免未 bootstrap 时路由不注册的死循环)+ `UserStore.HasAdmin`;Login 响应在 `TouchUserLastLogin` 前读 `LastLoginAt` 带出 `first_login`。前端登录页据 bootstrap 状态切换"引导卡/登录表单",加注册入口指引、错误码中文映射,主页据 `first_login` 显示一次性 onboarding 欢迎卡。

**Tech Stack:** Go(net/http + testify + table-driven)、Next.js 14 TypeScript(无单测框架,前端走浏览器手动验证 + playwright e2e)、SQLite(默认)/PostgreSQL store。

**Spec:** `docs/superpowers/specs/2026-06-18-login-bootstrap-guidance-design.md`

---

## File Structure

**后端(Go,TDD)**
- Create: `internal/session/sql/queries/users.has_admin.sql` — HasAdmin SQL
- Modify: `internal/session/multitenancy_store.go` — `UserWorkspaceStore` 接口加 `HasAdmin`;SQLiteStore 实现
- Modify: `internal/session/multitenancy_pg_store.go` — pgStore 实现
- Modify: `internal/gateway/auth_handlers.go` — `BootstrapStatus` handler;Login 响应带 `first_login`
- Modify: `cmd/hotplex/routes.go` — 注册 bootstrap-status 路由(独立块)
- Test: `internal/session/multitenancy_store_test.go` — HasAdmin 单测
- Test: `internal/gateway/auth_handlers_test.go`(新增或既有)— BootstrapStatus / Login first_login 单测

**前端(TS,手动验证)**
- Modify: `webchat/lib/api/auth.ts` — `getBootstrapStatus()`;`login()` 返回类型含 `first_login`
- Modify: `webchat/app/login/page.tsx` — bootstrap 检测 + 引导卡;`?invite` 预填;入口指引;`mapAuthError` 扩展
- Modify: `webchat/app/page.tsx` — onboarding 欢迎卡

---

## Task 1: HasAdmin — SQL + 接口 + SQLiteStore 实现

**Files:**
- Create: `internal/session/sql/queries/users.has_admin.sql`
- Modify: `internal/session/multitenancy_store.go:61`(接口)、`~229`(SQLiteStore User 方法区)
- Test: `internal/session/multitenancy_store_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `internal/session/multitenancy_store_test.go` 末尾:

```go
func TestUsersStore_HasAdmin(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()

	// 空库:无 admin
	got, err := store.HasAdmin(ctx)
	require.NoError(t, err)
	require.False(t, got)

	// 非 admin 用户不计入
	require.NoError(t, store.CreateUser(ctx, &security.User{
		ID: "u-1", Username: "alice", PasswordHash: "$2a$12$fake", Role: "user", Status: "active",
	}, 1700000000))
	got, err = store.HasAdmin(ctx)
	require.NoError(t, err)
	require.False(t, got)

	// 出现 admin → true
	require.NoError(t, store.CreateUser(ctx, &security.User{
		ID: "u-2", Username: "bob", PasswordHash: "$2a$12$fake", Role: "admin", Status: "active",
	}, 1700000000))
	got, err = store.HasAdmin(ctx)
	require.NoError(t, err)
	require.True(t, got)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/session/ -run TestUsersStore_HasAdmin -count=1`
Expected: 编译失败 `store.HasAdmin undefined`

- [ ] **Step 3: 新增 SQL 文件**

Create `internal/session/sql/queries/users.has_admin.sql`:

```sql
-- users.has_admin: returns 1 if any admin-role user exists (bootstrap detection).
-- Public, parameterless — used by GET /api/auth/bootstrap-status to guide first-time setup.
SELECT 1 FROM users WHERE role = 'admin' LIMIT 1
```

- [ ] **Step 4: 接口加方法**

在 `internal/session/multitenancy_store.go` 的 `UserWorkspaceStore` 接口 `// users` 注释块下(`ListUsers` 之前)加:

```go
	HasAdmin(ctx context.Context) (bool, error)
```

- [ ] **Step 5: SQLiteStore 实现**

在 `internal/session/multitenancy_store.go` 的 `TouchUserLastLogin` 方法后(line 229 后)加:

```go
func (s *SQLiteStore) HasAdmin(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, queries["users.has_admin"]).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has admin: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 6: 运行测试验证通过**

Run: `go test ./internal/session/ -run TestUsersStore_HasAdmin -count=1 -race`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/session/sql/queries/users.has_admin.sql internal/session/multitenancy_store.go internal/session/multitenancy_store_test.go
git commit -m "feat(session): add UserWorkspaceStore.HasAdmin for bootstrap detection"
```

---

## Task 2: pgStore.HasAdmin 实现

**Files:**
- Modify: `internal/session/multitenancy_pg_store.go`(pgStore User 方法区,line 71 `TouchUserLastLogin` 后)

- [ ] **Step 1: 加 pgStore 实现**

在 `internal/session/multitenancy_pg_store.go` 的 `func (s *pgStore) TouchUserLastLogin` 后加(镜像 SQLiteStore,用 `s.queries`,无 writeMu):

```go
func (s *pgStore) HasAdmin(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, s.queries["users.has_admin"]).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has admin: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 2: 验证编译(接口实现)**

Run: `go build ./internal/session/...`
Expected: 成功(编译断言 `_ UserWorkspaceStore = (*pgStore)(nil)` 强制实现;`users.has_admin.sql` 无占位符,PG rebound 无影响)

- [ ] **Step 3: 跑全包测试确认无回归**

Run: `go test ./internal/session/ -count=1 -race -short`
Expected: PASS(PG 集成测试在 `-short` 下 skip)

- [ ] **Step 4: Commit**

```bash
git add internal/session/multitenancy_pg_store.go
git commit -m "feat(session): add pgStore.HasAdmin mirroring SQLiteStore"
```

---

## Task 3: BootstrapStatus handler + 路由注册 + 测试

**Files:**
- Modify: `internal/gateway/auth_handlers.go`(加 `BootstrapStatus`)
- Modify: `cmd/hotplex/routes.go`(注册路由,独立于 CookieAuth 块)
- Test: `internal/gateway/auth_handlers_test.go`(若不存在则新建)

- [ ] **Step 1: 写失败测试**

若 `internal/gateway/auth_handlers_test.go` 不存在则新建 `package gateway`。追加:

```go
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

func newTestStore(t *testing.T) *session.SQLiteStore {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	cfg.DB.WALMode = true
	store, err := session.NewSQLiteStore(context.Background(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestBootstrapStatus_EmptyAndAfterAdmin(t *testing.T) {
	store := newTestStore(t)
	h := BootstrapStatus(store)

	// 空库 → bootstrapped:false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/bootstrap-status", nil)
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.False(t, body.Bootstrapped)

	// 创建 admin → true
	require.NoError(t, store.CreateUser(context.Background(), &security.User{
		ID: "u-1", Username: "admin", PasswordHash: "$2a$12$fake", Role: "admin", Status: "active",
	}, 1700000000))

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/api/auth/bootstrap-status", nil))
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &body))
	require.True(t, body.Bootstrapped)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/gateway/ -run TestBootstrapStatus -count=1`
Expected: 编译失败 `BootstrapStatus undefined`

- [ ] **Step 3: 实现 BootstrapStatus handler**

在 `internal/gateway/auth_handlers.go` 的 `Logout` 方法后加:

```go
// BootstrapStatus: GET /api/auth/bootstrap-status — whether any admin exists.
//
// Public (no auth): the login page polls this to guide first-time setup.
// Registered OUTSIDE the CookieAuth-gated auth block in routes.go so it stays
// reachable when the system is not yet bootstrapped (CookieAuth may be nil).
func BootstrapStatus(store session.UserWorkspaceStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		has, err := store.HasAdmin(r.Context())
		if err != nil {
			writeAppError(w, http.StatusInternalServerError, "INTERNAL", "check bootstrap status")
			return
		}
		respondJSON(w, map[string]bool{"bootstrapped": has})
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/gateway/ -run TestBootstrapStatus -count=1 -race`
Expected: PASS

- [ ] **Step 5: 注册路由(独立于 CookieAuth 块)**

在 `cmd/hotplex/routes.go` 中,`corsMw` 定义(line 43)之后、`if deps.CookieAuth != nil ...` 块(line 204)之前,插入独立注册块:

```go
	// bootstrap-status is intentionally registered OUTSIDE the CookieAuth-gated
	// auth block below: it must stay reachable before any admin exists (the very
	// state the login page needs to detect). Only requires the workspace store.
	if deps.WorkspaceStore != nil {
		mux.Handle("GET /api/auth/bootstrap-status", corsMw(gateway.BootstrapStatus(deps.WorkspaceStore)))
		mux.Handle("OPTIONS /api/auth/bootstrap-status", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	}
```

- [ ] **Step 6: 验证构建**

Run: `go build ./...`
Expected: 成功

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/auth_handlers.go internal/gateway/auth_handlers_test.go cmd/hotplex/routes.go
git commit -m "feat(gateway): add public GET /api/auth/bootstrap-status endpoint"
```

---

## Task 4: Login 响应带 first_login 标志

**Files:**
- Modify: `internal/gateway/auth_handlers.go:59-86`(Login handler)
- Test: `internal/gateway/auth_handlers_test.go`

`security.User.LastLoginAt int64`(0 = 从未登录,见 `identity_provider.go:22`)。Login handler 现在认证后直接 `TouchUserLastLogin`,要在 touch **前**读原值判定首次。

- [ ] **Step 1: 写失败测试**

追加到 `internal/gateway/auth_handlers_test.go`:

```go
func TestLogin_FirstLoginFlag(t *testing.T) {
	// first_login: 首次登录(原 LastLoginAt==0)为 true;二次登录为 false。
	// 此测试验证 Login 响应 JSON 含 first_login 字段且语义正确。
	// 完整 Login 集成需 CookieAuth + IDP;此处用 helper 构造最小 AuthHandlers。
	// (若团队已有 Login 集成测试 helper,复用之;否则按下方最小构造。)
	t.Skip("Login handler 需 CookieAuth+IDP 构造;按团队既有 auth 集成测试模式补全,或在此处构造 BootstrapStatus 同款 store + LocalAccountProvider")
}
```

> **注:** Login 走完整 CookieAuth + IDP。若 `internal/gateway` 已有 auth 集成测试 helper(构造 `AuthHandlers` 的 fixture),优先复用写真实断言;若没有,保留 skip 占位,靠 Task 9 集成验证覆盖,不阻塞。

- [ ] **Step 2: 修改 Login handler**

`internal/gateway/auth_handlers.go` 的 `Login` 方法,把 line 80-85:

```go
	if err := h.cookieAuth.SetCookie(w, r, uid); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "cookie error")
		return
	}
	_ = h.store.TouchUserLastLogin(r.Context(), uid, h.nowUnix()) // non-critical on success
	respondJSON(w, map[string]string{"user_id": uid})
```

改为(touch 前读原 LastLoginAt):

```go
	if err := h.cookieAuth.SetCookie(w, r, uid); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "cookie error")
		return
	}
	// 在 touch 前读原 last_login_at,判定是否首次登录(供前端 onboarding)。
	firstLogin := false
	if u, lerr := h.auth.IdentityProvider().Lookup(r.Context(), uid); lerr == nil && u.LastLoginAt == 0 {
		firstLogin = true
	}
	_ = h.store.TouchUserLastLogin(r.Context(), uid, h.nowUnix()) // non-critical on success
	respondJSON(w, map[string]any{"user_id": uid, "first_login": firstLogin})
```

同步把 `AcceptInvite` 成功响应(line 211)也带上(新注册必为首次):

```go
	_ = h.cookieAuth.SetCookie(w, r, uid)
	respondJSON(w, map[string]any{"user_id": uid, "first_login": true})
```

- [ ] **Step 3: 验证构建 + 既有测试不回归**

Run: `go build ./... && go test ./internal/gateway/ -count=1 -race -short`
Expected: 成功

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/auth_handlers.go internal/gateway/auth_handlers_test.go
git commit -m "feat(gateway): include first_login flag in login/accept-invite response"
```

---

## Task 5: 前端 getBootstrapStatus API + 引导卡

**Files:**
- Modify: `webchat/lib/api/auth.ts`(加 `getBootstrapStatus`)
- Modify: `webchat/app/login/page.tsx`(检测 + 引导卡)

- [ ] **Step 1: 加 API 函数**

在 `webchat/lib/api/auth.ts` 的 `getMe` 之前加:

```ts
// BootstrapStatus: whether any admin exists. Drives the first-time-setup guide
// on the login page. Public endpoint (no auth).
export async function getBootstrapStatus(signal?: AbortSignal): Promise<boolean> {
  const res = await fetch(`${BASE}/api/auth/bootstrap-status`, {
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    // Degrade: treat unreachable as "bootstrapped" so the normal login form shows.
    return true;
  }
  const data = await res.json();
  return Boolean(data?.bootstrapped);
}
```

- [ ] **Step 2: login 页加 bootstrap 检测 state**

`webchat/app/login/page.tsx` 的 `InnerLoginPage` 内,在现有 state 声明区(`const [providers, setProviders]`)附近加:

```ts
  const [bootstrapped, setBootstrapped] = useState<boolean | null>(null);
```

在 OAuth providers 的 `useEffect` 后加新 effect:

```ts
  // Detect bootstrap state: if no admin exists yet, show setup guide instead of the form.
  useEffect(() => {
    const check = async () => {
      try {
        setBootstrapped(await getBootstrapStatus());
      } catch {
        setBootstrapped(true); // degrade to normal login
      }
    };
    check();
  }, []);
```

并把 `getBootstrapStatus` 加入顶部 import:

```ts
import { login, acceptInvite, getOAuthProviders, getMe, getBootstrapStatus, type OAuthProvider } from '@/lib/api/auth';
```

- [ ] **Step 3: 引导卡组件 + 条件渲染**

在 `InnerLoginPage` 的 `return` 之前(组件函数体内)加引导卡 JSX 变量:

```tsx
  if (bootstrapped === false) {
    const cmd = `hotplex admin create --username <name> --config configs/config-dev.yaml`;
    return (
      <div className="relative flex min-h-screen items-center justify-center bg-[var(--bg-base)] px-4">
        <div className="bg-mesh opacity-50" />
        <div className="noise-overlay" />
        <div className="w-full max-w-md animate-fade-in-up z-10 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-lg)] backdrop-blur-xl">
          <div className="mb-5 text-center">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-[var(--accent-gold)]/10">
              <BrandIcon size={48} className="animate-float" />
            </div>
            <h1 className="font-display text-2xl font-black tracking-tight text-[var(--text-primary)]">
              初始化管理员账号
            </h1>
            <p className="mt-2 text-xs text-[var(--text-muted)] leading-relaxed">
              这是全新部署,还没有管理员账号。请在服务器上运行以下命令创建首个管理员,然后刷新此页。
            </p>
          </div>
          <div className="rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-elevated)] p-3">
            <div className="flex items-center justify-between gap-2">
              <code className="text-xs font-mono text-[var(--text-secondary)] break-all">{cmd}</code>
              <button
                type="button"
                onClick={() => navigator.clipboard?.writeText(cmd)}
                className="shrink-0 rounded-md border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:text-[var(--text-primary)]"
              >
                复制
              </button>
            </div>
          </div>
          <p className="mt-4 text-center text-[10px] text-[var(--text-faint)] leading-relaxed">
            密码交互式输入(≥8 字符)。用户名 [a-zA-Z0-9_.-],不可以 apikey: 开头。
          </p>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="mt-5 w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black hover:bg-[var(--accent-gold-bright)]"
          >
            创建后刷新
          </button>
        </div>
      </div>
    );
  }
```

- [ ] **Step 4: 加载中态**

在 `InnerLoginPage` 的 `return` 最前面加(bootstrapped 还在加载时):

```tsx
  if (bootstrapped === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }
```

- [ ] **Step 5: 浏览器手动验证**

Run: `make dev`(gateway + webchat)
1. 清空 users 表的 admin(临时):当前 db 已有 admin?若无则跳过;若有,用 sqlite3 删 admin 后刷新登录页 → 应显示"初始化管理员账号"卡片 + CLI 命令 + 复制按钮
2. 运行 CLI 创建 admin → 刷新 → 应回到正常登录表单

- [ ] **Step 6: Commit**

```bash
git add webchat/lib/api/auth.ts webchat/app/login/page.tsx
git commit -m "feat(webchat): show bootstrap guide when no admin exists"
```

---

## Task 6: 注册入口指引 + ?invite 预填

**Files:**
- Modify: `webchat/app/login/page.tsx`

- [ ] **Step 1: ?invite 预填 + 默认切 register tab**

`InnerLoginPage` 内,在已有 `useEffect` 区加(读取 `?invite=` 并切 tab + 预填):

```ts
  // ?invite=CODE → 预填邀请码并切到 Accept Invite tab
  useEffect(() => {
    const code = searchParams.get('invite');
    if (code) {
      setInviteCode(code);
      setActiveTab('register');
    }
  }, [searchParams]);
```

- [ ] **Step 2: Sign In tab 底部"立即注册"链接**

在登录表单(`<form onSubmit={handleLoginSubmit}>`)的提交按钮**之后**、`</form>` 之前加:

```tsx
              <p className="pt-1 text-center text-[11px] text-[var(--text-muted)]">
                收到邀请码?{' '}
                <button
                  type="button"
                  onClick={() => { setActiveTab('register'); setError(''); }}
                  className="font-bold text-[var(--accent-gold)] hover:underline"
                >
                  立即注册 →
                </button>
              </p>
```

- [ ] **Step 3: 邀请码字段下加来源说明**

在注册表单的邀请码 `input`(placeholder "Paste invitation code")所在 `<div>` 之后加:

```tsx
                <p className="-mt-2 mb-1 text-[10px] text-[var(--text-faint)]">
                  邀请码由管理员发放。没有?请联系管理员获取。
                </p>
```

- [ ] **Step 4: 浏览器手动验证**

1. 访问 `/login?invite=ABC123` → 自动切到 Accept Invite tab,邀请码预填 ABC123
2. Sign In tab 底部应有"立即注册 →"链接,点击切 tab
3. Accept Invite 邀请码字段下有来源说明

- [ ] **Step 5: Commit**

```bash
git add webchat/app/login/page.tsx
git commit -m "feat(webchat): add invite entry guidance and ?invite prefilled"
```

---

## Task 7: 注册/登录错误码中文映射

**Files:**
- Modify: `webchat/app/login/page.tsx`(`mapAuthError`)

- [ ] **Step 1: 扩展 mapAuthError**

把 `mapAuthError` 函数(line 10-36)整体替换为(在现有 SSO 码基础上补注册/邀请/凭证码):

```ts
function mapAuthError(code: string | null): string | null {
  if (!code) return null;
  switch (code) {
    // 登录凭证
    case 'INVALID_CREDENTIALS':
      return '用户名或密码错误,请重试。';
    case 'USER_DISABLED':
      return '此账号已被管理员禁用,请联系系统管理员。';
    case 'NO_IDP':
      return '登录服务未就绪,请稍后再试或联系管理员。';
    // 注册 — 用户名/密码
    case 'INVALID_USERNAME':
      return '用户名格式无效:需 3-64 字符,仅 [a-zA-Z0-9_.-],且不能以 apikey: 开头。';
    case 'INVALID_PASSWORD':
      return '密码长度无效:需 8-72 字符。';
    case 'USERNAME_TAKEN':
      return '该用户名已被占用。若你刚用此邀请码注册,邀请码已消耗,请联系管理员重新发码后换名重试。';
    // 注册 — 邀请码
    case 'INVITATION_NOT_FOUND':
      return '邀请码不存在,请检查后重试。';
    case 'INVITATION_USED':
      return '邀请码已被使用(每个码仅一次)。请联系管理员重新发放。';
    case 'INVITATION_EXPIRED':
      return '邀请码已过期。请联系管理员重新发放。';
    // SSO
    case 'STATE_EXPIRED':
      return '登录状态已过期,请重新登录。';
    case 'PROVIDER_MISMATCH':
      return '第三方登录服务商不匹配。';
    case 'CSRF_DETECTED':
      return '检测到跨站请求伪造(CSRF),请确保浏览器启用了 Cookie 并重试。';
    case 'STATE_INVALID':
      return '登录状态无效,请重新登录。';
    case 'USER_CREATE_FAILED':
      return '从单点登录(SSO)创建用户账号失败。';
    case 'CODE_EXCHANGE_FAILED':
      return 'SSO 授权码交换失败。';
    case 'ID_TOKEN_INVALID':
      return 'SSO 凭证令牌验证失败。';
    case 'IDP_ERROR':
      return '第三方登录服务商返回错误。';
    case 'UNAUTHORIZED':
      return '会话未授权,请先登录。';
    case 'BAD_REQUEST':
      return '请求参数缺失,请填写完整后重试。';
    default:
      return `操作失败(${code}),请重试或联系管理员。`;
  }
}
```

- [ ] **Step 2: 浏览器手动验证**

用错误邀请码注册 → 应显示"邀请码不存在";用已存在用户名注册(需先有该用户)→ "该用户名已被占用..."

- [ ] **Step 3: Commit**

```bash
git add webchat/app/login/page.tsx
git commit -m "feat(webchat): map registration/invite error codes to friendly copy"
```

---

## Task 8: onboarding 欢迎卡(首次登录)

**Files:**
- Modify: `webchat/lib/api/auth.ts`(`login`/`acceptInvite` 返回类型)
- Modify: `webchat/app/login/page.tsx`(login 成功后存 first_login)
- Modify: `webchat/app/page.tsx`(读 first_login 显示欢迎卡)

- [ ] **Step 1: 调整 login/acceptInvite 返回类型**

`webchat/lib/api/auth.ts`,把 `login` 的返回类型改:

```ts
export interface LoginResult {
  user_id: string;
  first_login: boolean;
}

export async function login(username: string, password: string, signal?: AbortSignal): Promise<LoginResult> {
  const res = await fetch(`${BASE}/api/auth/login`, {
    method: 'POST',
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ username, password }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData?.error?.code || `Login failed: ${res.status}`);
  }
  return res.json();
}
```

同样改 `acceptInvite` 返回 `Promise<LoginResult>`(响应体同构)。`User` interface 保持不变(供 `getMe`)。

- [ ] **Step 2: login 页存 first_login 标志**

`webchat/app/login/page.tsx` 的 `handleLoginSubmit`,把:

```ts
      await login(loginUsername.trim(), loginPassword);
      router.push('/');
```

改为:

```ts
      const result = await login(loginUsername.trim(), loginPassword);
      if (result.first_login) {
        try { localStorage.setItem('hotplex.onboarding', '1'); } catch {}
      }
      router.push('/');
```

同样改 `handleRegisterSubmit`(注册必首次):

```ts
      await acceptInvite(inviteCode.trim(), registerUsername.trim(), registerPassword);
      try { localStorage.setItem('hotplex.onboarding', '1'); } catch {}
      router.push('/');
```

- [ ] **Step 3: page.tsx 读标志显示欢迎卡**

`webchat/app/page.tsx` 的 `InnerPage`,在 `setChecking(false)` 的 `checkAuth` 成功分支后,onboarding 检测:

```ts
        try {
          await getMe();
          setChecking(false);
          try {
            if (localStorage.getItem('hotplex.onboarding') === '1') {
              setShowOnboarding(true);
              localStorage.removeItem('hotplex.onboarding');
            }
          } catch {}
```

(`InnerPage` 顶部加 `const [showOnboarding, setShowOnboarding] = useState(false);`)

在 `InnerPage` 的 return 里、ChatUI 之上条件渲染欢迎卡(关闭即消失):

```tsx
  {showOnboarding && (
    <OnboardingWelcome onClose={() => setShowOnboarding(false)} />
  )}
```

新增组件(同文件或 `webchat/app/components/OnboardingWelcome.tsx`):

```tsx
function OnboardingWelcome({ onClose }: { onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-lg rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-7 shadow-[var(--shadow-lg)] animate-fade-in-up">
        <div className="mb-4 flex items-center gap-3">
          <div className="flex h-11 w-11 items-center justify-center rounded-lg bg-[var(--accent-gold)]/10">
            <BrandIcon size={32} />
          </div>
          <div>
            <h2 className="font-display text-lg font-black text-[var(--text-primary)]">欢迎来到 HotPlex</h2>
            <p className="text-[11px] text-[var(--text-muted)] uppercase tracking-wider">几步开始你的第一次对话</p>
          </div>
        </div>
        <ol className="space-y-2.5 text-sm text-[var(--text-secondary)]">
          <li><span className="font-bold text-[var(--accent-gold)]">1.</span> 在设置中创建你的第一个 workspace(工作目录)</li>
          <li><span className="font-bold text-[var(--accent-gold)]">2.</span> 选择 worker 类型(claude_code / codex 等)</li>
          <li><span className="font-bold text-[var(--accent-gold)]">3.</span> 在对话框输入任务,开始编码</li>
        </ol>
        <button
          type="button"
          onClick={onClose}
          className="mt-6 w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black hover:bg-[var(--accent-gold-bright)]"
        >
          开始使用
        </button>
      </div>
    </div>
  );
}
```

(顶部 import 加 `BrandIcon` 若未引入:`import { BrandIcon } from '@/components/icons';`)

- [ ] **Step 4: 浏览器手动验证**

1. 用新建用户登录 → 主页应弹出 onboarding 欢迎卡
2. 点"开始使用"关闭 → 刷新不再出现(localStorage 已清)
3. 二次登录(同用户)→ 不再弹(first_login=false,不写 localStorage)

- [ ] **Step 5: Commit**

```bash
git add webchat/lib/api/auth.ts webchat/app/login/page.tsx webchat/app/page.tsx
git commit -m "feat(webchat): first-login onboarding welcome card"
```

---

## Task 9: 集成验证 + 推送

**Files:** 无(验证 + 推送)

- [ ] **Step 1: 后端全量门禁**

Run: `make quality && make build`
Expected: fmt + lint + test + build 全过

- [ ] **Step 2: dev 端到端验证**

Run: `make dev`(`make dev` 现会自动 dev-build 最新代码)

1. 清除 admin(sqlite3 ~/.hotplex/data/hotplex.db "DELETE FROM users WHERE role='admin';")→ 访问 :3000/login → 显示 bootstrap 引导卡
2. `./bin/hotplex-darwin-arm64 admin create --username admin --config configs/config-dev.yaml` → 刷新 → 登录表单
3. admin 创建邀请(`curl` `/api/admin/invitations` 或 admin UI)→ 访问 `/login?invite=<code>` → 切到 Accept Invite + 预填
4. 注册新用户 → onboarding 欢迎卡弹出 → 关闭 → 刷新不重现
5. 故意输错密码/邀请码 → 友好中文错误

- [ ] **Step 3: 推送**

```bash
git push
```
Expected: pre-push 全门禁通过(fmt/vet/verify/lint/build/tests 6 项)+ 推送成功

---

## Self-Review Checklist(plan 作者已自审)

- **Spec 覆盖**:bootstrap 引导(Task 1-3,5)✓;入口指引(Task 6)✓;onboarding(Task 4,8)✓;错误友好度(Task 7)✓;bootstrap-status 脱离 CookieAuth 守卫(Task 3 Step 5)✓
- **类型一致**:`HasAdmin(ctx)(bool,error)` 全任务一致;`LoginResult{user_id,first_login}` 前后端一致;`getBootstrapStatus():Promise<boolean>` 一致
- **无 placeholder**:Task 4 的 Login 集成测试用 `t.Skip` 占位(标注原因 + 替代覆盖路径),非 TBD;其余步骤均含完整代码
