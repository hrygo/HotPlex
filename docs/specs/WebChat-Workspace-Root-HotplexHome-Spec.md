# WebChat Workspace 沙箱根跟随 HOTPLEX_HOME 修订规格

**状态**: draft（设计待评审，v3）
**日期**: 2026-08-13
**分支**: `feat/configurable-workspace`
**前置文档**: [`WebChat-Workspace-Create-WorkDir-Prefix-Spec.md`](./WebChat-Workspace-Create-WorkDir-Prefix-Spec.md)、[`../superpowers/plans/2026-08-12-configurable-workspace-default-config-path.md`](../superpowers/plans/2026-08-12-configurable-workspace-default-config-path.md)
**Supersedes**: `WebChat-Workspace-Create-WorkDir-Prefix-Spec.md` §5.1.1 的沙箱前缀定义（`$HOME/.hotplex/workspaces/<owner_user_id>` → `HotplexHome()/workspaces/<segment>`）

---

## 1. 背景

`HOTPLEX_HOME` 可配置化（`feat/configurable-workspace`，plan 见前置文档）的目标是"**一个环境变量控制整个 workspace（含配置文件、数据、日志、PID）**"。`config.HotplexHome()`（`internal/config/config.go:169`）已作为所有状态目录的唯一事实源。

但 webchat workspace 沙箱仍硬编码在真实用户主目录下，**不跟随 `HOTPLEX_HOME`**，造成三处断裂：

| # | 断裂点 | 位置 | 后果 |
|---|--------|------|------|
| B1 | 沙箱 base 用 `os.UserHomeDir()` + 硬编码 `.hotplex` | `internal/security/path.go:106` | 设置 `HOTPLEX_HOME=/data/hotplex` 后，默认会话目录落于 `/data/hotplex/workspace`，webchat 新建 workspace 却落于 `$HOME/.hotplex/workspaces/<segment>/`——**数据散落两处** |
| B2 | 错误消息硬编码 `$HOME/.hotplex/workspaces/...` | `workspace_handlers.go:124`、`:237` | 用户看到的提示与实际沙箱根不一致 |
| B3 | 前端硬编码 `~/.hotplex/workspaces/<segment>/` 构造路径 | `webchat/lib/utils/workspace-path.ts:28`、`NewWorkspaceForm.tsx:31` | 前端**无渠道感知**服务端 `HOTPLEX_HOME`；一旦后端沙箱跟随 `HotplexHome()`，前端构造的路径将全部 403 `WORK_DIR_OUTSIDE_SANDBOX`，webchat 创建 workspace 功能整体瘫痪 |

**联动风险（关键）**：B3 与 B1 互为因果——只修后端（沙箱跟随 `HotplexHome()`）不修前端，webchat 创建功能 100% 回归；只修前端不修后端，语义分裂依旧。必须前后端同步，且前端需要一个获取 workspace 根的服务端渠道（当前不存在）。

**目录段标识（评审修订 v2 → v3）**：沙箱根末段由 `owner_user_id`（UUID）改为可读的 `username` 派生段（`workspaces/alice/`）。v3 依据评审将映射规则升级为**四身份空间隔离编码**（§5.1.2），覆盖密码/OAuth/机器/系统四类身份来源。

---

## 2. 现状调研

### 2.1 `HOTPLEX_HOME` 覆盖度（当前分支已完成的）

| 目录 | 跟随 HOTPLEX_HOME | 出处 |
|------|:---:|------|
| 默认配置 `config.yaml` | ✅ | `config.go:162`（`DefaultConfigPath()`，commit 9bde4493） |
| 数据 `data/`、日志 `logs/`、PID `.pids/` | ✅ | `config_defaults.go:32,42,59`、`gateway_run.go:1044` |
| scripts/agent-configs/skills | ✅ | `config_defaults.go:147,173`、`gateway_run.go:243` |
| 默认工作目录 `Worker.DefaultWorkDir` | ✅ | `config_defaults.go:58` → `HotplexHome()/workspace` |
| **workspace 沙箱 `workspaces/<segment>`** | ❌ | `security/path.go:106` 硬编码 `$HOME/.hotplex` |

### 2.2 硬编码点清单

| # | 文件:行 | 内容 |
|---|---------|------|
| H1 | `internal/security/path.go:106` | `base := filepath.Join(home, ".hotplex", "workspaces", ownerUserID)`（`home = os.UserHomeDir()`） |
| H2 | `internal/gateway/workspace_handlers.go:124` | `"work_dir must be under $HOME/.hotplex/workspaces/<your-user-id>"` |
| H3 | `internal/gateway/workspace_handlers.go:237` | `"work_dir must be under the workspace owner's sandbox ($HOME/.hotplex/workspaces/<owner-user-id>)"` |
| H4 | `webchat/lib/utils/workspace-path.ts:28` | `workspaceSandboxPrefix(ownerId)` 返回 `~/.hotplex/workspaces/${ownerId}/` |
| H5 | `webchat/app/components/chat/NewWorkspaceForm.tsx:31` | 占位符 `~/.hotplex/workspaces/${uid}/…` |
| H6 | `internal/gateway/workspace_handlers_test.go:28` | 测试辅助函数 `filepath.Join(home, ".hotplex", "workspaces", ownerUserID, sub)` |
| H7 | `cmd/hotplex/gateway_run.go:1655` | skills 目录 `filepath.Join(home, ".hotplex", "skills")`（同类问题，P2） |
| H8 | `cmd/hotplex/messaging_init.go:255` | phrases 目录 `filepath.Join(homeDir, ".hotplex", "phrases")`（同类问题，P2） |

### 2.3 身份事实（四类身份来源，目录段设计依据）

| # | 身份来源 | username 形态 | 约束证据 |
|---|----------|--------------|----------|
| I1 | 密码注册（webchat accept-invite / admin CLI） | `[a-zA-Z0-9_.-]{3,64}` | `ValidateUsername`，`identity_provider.go:100-112`；**无改名路径**（IdP 同步只动 display_name/email，`multitenancy_store.go:666-670`） |
| I2 | OAuth / SSO | `provider:subject`（**含 `:`，绕过 ValidateUsername**） | `oauth_handlers.go:213-217`（`username := providerName + ":" + claims.Subject`）；provider 名来自配置（`oauth_types.go:54-64`，未限制）；subject 来自 IdP claims（**可含 `/`、`..`、`:`**） |
| I3 | 机器用户（API-key provision） | `apikey:<user_id>`（user_id 任意字符串，仅长度限制） | migration `018`；`apikey_handlers.go:381-384` |
| I4 | 系统身份（无 users 行） | `anonymous`（dev 模式）、`api_user`（resolver 未映射的 API key） | `auth.go:186-192,334-341`；认证层对二者**跳过 Lookup**（`auth.go:207,263`） |

**关键推论**：
- `users.username` 全局 `UNIQUE NOT NULL`（migration `017:7`）——username 唯一，但**目录段是 username 的变换**，变换非单射（`:`→`-`）时唯一性不自动传递（v2 遗留风险，v3 已解决，§5.1.2）。
- 认证层保证：能通过 `requireAuth` 的 uid 要么是系统身份（I4），要么 `IdentityProvider.Lookup` 必然成功（非系统身份 Lookup 失败在 `auth.go:207-215` 已 401）——**sandboxRootFor 无需 403 失败路径**（v3 修正 v2 缺陷）。

### 2.4 依赖方向确认

`internal/security` **已 import** `internal/config`（`path_unix.go`、`auth.go`、`cookie.go`、`oauth_manager.go` 均引用）——沙箱根改用 `config.HotplexHome()` **无循环依赖风险**。

### 2.5 前端工作目录感知现状

`webchat/lib/config.ts:70` 的 `workDir` 来自构建期 `HOTPLEX_WEBCHAT_WORK_DIR` env，与运行期 `HOTPLEX_HOME` 无关联；前端无任何 API 获取 gateway 的 `HotplexHome()`。`listWorkspaces()`（`webchat/lib/api/workspaces.ts:61`）响应当前不含 `workspace_root`。

---

## 3. 目标 / 非目标

### 目标

- **G1（规范性）**：workspace 沙箱根与 `HotplexHome()` 同源——`HotplexHome()/workspaces/<segment>`，一个环境变量控制整个 workspace（含 webchat workspace）。
- **G1a（可读性）**：沙箱末段由 username 派生（`workspaces/alice/`），目录在 `ls`/日志/排障时可识别归属。
- **G1b（安全）**：目录段对**全部四类身份来源**（密码/OAuth/机器/系统）不相交且 filesystem-safe（§5.1.2），**空间内注入（sanitize 有损/大小写差异经稳定哈希消除碰撞）**，杜绝跨用户目录段碰撞越权。
- **G2（灵活性）**：服务端通过 API 暴露 workspace 根（绝对路径），前端动态构造路径；subdir 支持多级相对段（如 `projects/myapp`）。
- **G3（易用性）**：创建表单路径预览/占位符动态化；admin 创建时可选 `permission_mode`。
- **G4（兼容性）**：存量 workspace 记录 grandfather，零迁移；`HOTPLEX_HOME` 未设置时沙箱根 = `~/.hotplex/workspaces/<segment>`（段名由 UUID 变为 username 派生是有意变更，见 §5.4）。

### 非目标

- **不做数据迁移**：存量 workspace 的 `work_dir` 绝对路径原样保留（含存量 UUID 目录段，§5.4）。
- **不改 POST/PATCH API 契约**：`{name, work_dir, permission_mode}` 不变，保护 SDK 消费者。
- **不做用户名改名**：username 不可变是安全前置条件（P2）；改名能力（若未来引入）不属于本规格。
- **不迁移存量 `apikey-*`/`oauth-*` 密码用户**：已注册用户 grandfather（§5.4），仅新注册受保留前缀约束。
- **不引入配置文件内 `home:` 字段**：沿用前置 plan 的否决结论。
- **P2 项（H7/H8）不阻塞本规格交付**：独立提交，见 §5.7。

---

## 4. 决策记录

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 沙箱 base 来源 | `config.HotplexHome()`（替代 `os.UserHomeDir()/.hotplex`） | 与 `DefaultWorkDir`/data/logs/PID 同源；security 已 import config，无依赖问题 |
| 目录段标识（v2） | `username` 派生段（替代 owner_user_id UUID） | UUID 目录不可读、排障体验差；username 可读 |
| **目录段编码（v3）** | **四身份空间隔离**：密码=username 原样；机器=`apikey-<sanitized>`；OAuth=`oauth-<provider>-<subject>`；系统=uid 字面量（§5.1.2） | `:`→`-` 简单替换非单射：OAuth `apikey:<subject>` 与机器 `apikey:<uuid>` 碰撞、OAuth subject 可含 `/` 路径逃逸、密码可注册 `apikey-*`——四空间两两不相交且 filesystem-safe 才能杜绝跨用户目录段碰撞越权 |
| **P1：命名空间保留** | `ValidateUsername` 拒绝 `apikey:`（已有）、`apikey-` 前缀、`oauth-` 前缀、字面量 `anonymous`/`api_user`；OAuth provider 名禁止 `apikey`/`oauth`（配置校验） | 封闭密码注册/OAuth 两入口对机器/系统空间的侵入；静态不可相交，零 TOCTOU |
| **P2：username 不可变冻结** | username 创建后不可变，作为安全前置条件写入本规格 | 沙箱根依赖 username；现状无改名路径（§2.3 I1），冻结语义即可 |
| **P3：存量根 grandfather** | 存量 UUID 目录 workspace 原样保留；新建 workspace 落新段根 | 与既有 grandfather 语义一致；旧根 workspace 的 work_dir 更新按新 base 校验 403（其他字段不受影响） |
| **P4：系统身份 fallback（v3 修正）** | `sandboxRootFor`：idp nil / `anonymous` / `api_user` → 目录段 = uid 字面量；Lookup 失败（认证层已拦截，防御性）→ 同 uid | 认证层对系统身份跳过 Lookup 且认证成功（`auth.go:207,263`）；v2 的 403 路径会破坏 dev 模式与未映射 API key 的 workspace 创建（现状可用） |
| base 计算收敛 | 导出单一 API `security.WorkspaceSandboxRoot(username string)`，**内部执行** `sandboxDirSegment` 映射并 `filepath.Abs` | 防调用方误用未映射段；`HotplexHome()` 对 `HOTPLEX_HOME` 原样返回（`config.go:172-173`），不保证绝对路径，内部 Abs 兜底 |
| `workspace_root` 传输形式 | **绝对路径**（非 `~/` 形式） | 前端浏览器进程的 `$HOME` 可能与 gateway 进程不同（远程部署）；`~` 展开必须在服务端完成 |
| 暴露渠道 | `GET /api/workspaces` List 响应新增 `workspace_root` 字段 | 前端初始化必然调用 `listWorkspaces()`，零新端点；字段向后兼容 |
| **前端锚点反解（v3 修正）** | `resolveSandboxAnchor(workDir)` 从 work_dir 提取 `workspaces/<segment>/`（无身份参数） | 普通 List 响应无 `owner_username`（仅 admin projection 有，`multitenancy_store.go:30-39`）；自提取天然兼容 UUID grandfather 根与 username 根；调用点（`general-tab.tsx:44,66`）不再需要身份数据 |
| subdir 多段 | 前端按 `/` 分段 sanitize（逐段 `[a-z0-9-]`），后端沙箱校验天然支持多级 | 后端 `filepath.Rel` 纯字符串逻辑；`..` 段由后端 403 兜底 |
| `permission_mode` 可选 | 创建表单 admin 可见（复用 `worker.ValidatePermissionMode`），非 admin 隐藏 | 既有 Create handler 已支持（`workspace_handlers.go:101-112`），仅补 UI |
| 错误消息 | 动态拼接 `WorkspaceSandboxRoot(segment)` | 消除 H2/H3 字面量 |

---

## 5. 设计

### 5.1 后端：沙箱根收敛

#### 5.1.1 新增 `security.WorkspaceSandboxRoot`（`internal/security/path.go`）

```go
// WorkspaceSandboxRoot 返回 username 的 workspace 沙箱根目录：
//
//	HotplexHome()/workspaces/<sandboxDirSegment(username)>
//
// 与 HotplexHome() 同源（跟随 HOTPLEX_HOME，未设置时回退 ~/.hotplex）；
// 内部执行目录段映射（§5.1.2）与 filepath.Abs（HOTPLEX_HOME 可能为相对值），
// 是所有 workspace 路径校验/错误消息/API 暴露的唯一事实源。
func WorkspaceSandboxRoot(username string) string {
	base := filepath.Join(config.HotplexHome(), "workspaces", sandboxDirSegment(username))
	abs, err := filepath.Abs(base)
	if err != nil { // 不可达（HotplexHome 恒可拼接）；防御性保留原值
		return base
	}
	return abs
}
```

#### 5.1.2 目录段映射规则：四身份空间隔离（G1a + G1b + P1）

| 身份类 | username 形态 | 目录段 | 变换 |
|--------|--------------|--------|------|
| 系统身份（I4） | `anonymous` / `api_user`（uid 字面量，无 users 行） | `anonymous` / `api_user` 原样 | 恒等；**密码注册字面量被 P1 封锁** |
| 密码用户（I1） | `[a-zA-Z0-9_.-]{3,64}`，非保留前缀 | username 原样 | 恒等（`ValidateUsername` 已保证） |
| 机器用户（I3） | `apikey:<user_id>` | `apikey-<seg(user_id)>` | `apikey:` 前缀 + user_id 逐字符 sanitize |
| OAuth 用户（I2） | `provider:subject` | `oauth-<seg(provider:subject)>` | 显式 `oauth-` 前缀 + 分段 sanitize |

**段注入性（G1b，v3 评审 F1/R6 修订）**：`seg` 为 sanitize 恒等**且全小写且非空且非 digest 输出形态**时原样返回（可读性保留）；否则追加**完整 SHA-256 十六进制摘要**（64 字符；评审第二轮否决 4-byte 截断——32 位前缀生日碰撞可被暴力构造），保证**同空间内**不同身份映射到不同目录段（`github:user/1` 与 `github:user-1` 不再碰撞；`Alice` 与 `alice` 在大小写不敏感文件系统上不再碰撞；空输入退化为纯摘要段）。**跨分支碰撞（R6）**：digest 分支输出形态本身是合法恒等输入（`"Abc"` → `"abc-<h>"` 与恒等输入 `"abc-<h>"`；空 base 变体 `"!!!"` → `"<h>"` 与恒等输入 `"<h>"`），恒等分支排除 digest 形态（`-[0-9a-f]{64}$` 后缀 / 纯 64-hex）后，digest 输出集合与恒等输出集合不相交，映射恢复注入。

```go
// sandboxDirSegment 将 username 映射为沙箱目录段（四空间隔离 + 空间内注入，filesystem-safe）：
//   - 无 ":" → 原样（密码用户经 ValidateUsername 已保证 [a-zA-Z0-9_.-]；
//     系统身份 anonymous/api_user 同属此路径，字面量被 P1 封锁给密码用户）
//   - "apikey:" 前缀 → 机器用户："apikey-" + lossySafeSegment(rest)
//   - 其他含 ":" → OAuth 用户："oauth-" + lossySafeSegment(whole)
func sandboxDirSegment(username string) string {
	if i := strings.IndexByte(username, ':'); i >= 0 {
		prefix := username[:i]
		switch prefix {
		case "apikey":
			return "apikey-" + lossySafeSegment(username[i+1:])
		default: // OAuth provider:subject
			return "oauth-" + lossySafeSegment(username)
		}
	}
	return lossySafeSegment(username)
}

// sanitizePathSegment：非 [a-zA-Z0-9_.-] 字符替换为 "-"，连续折叠、去首尾；
// 结果为 "." / ".."（路径逃逸段）时返回空串。
func sanitizePathSegment(s string) string { ... }

// lossySafeSegment 返回 filesystem-safe 且注入（无碰撞）的目录段：
// sanitize 恒等且全小写且非空且非 digest 输出形态 → 原样；否则追加完整
// SHA-256 十六进制摘要（64 字符，碰撞等价于 SHA-256 碰撞；空段/空输入
// 退化为纯摘要段）。恒等分支排除 digest 形态输入（R6 跨分支碰撞）。
var (
	digestSuffixRe = regexp.MustCompile(`-[0-9a-f]{64}$`)
	pureDigestRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func lossySafeSegment(s string) string {
	seg := sanitizePathSegment(s)
	if seg == s && seg == strings.ToLower(s) && s != "" &&
		!digestSuffixRe.MatchString(s) && !pureDigestRe.MatchString(s) {
		return seg
	}
	sum := sha256.Sum256([]byte(s))
	base := strings.ToLower(seg)
	if base == "" {
		return hex.EncodeToString(sum[:])
	}
	return base + "-" + hex.EncodeToString(sum[:])
}
```

> **P1 落地清单**（封闭碰撞面）：
> 1. `ValidateUsername`（`identity_provider.go:100-112`）扩展保留规则：拒绝 `apikey:`（已有）、**`apikey-` 前缀、`oauth-` 前缀、字面量 `anonymous`、`api_user`**。
> 2. OAuth provider 名配置校验（`oauth_types.go:54-64` 或注册处）：**禁止 `apikey`、`oauth`**（防 `apikey:<subject>` 侵入机器空间；`oauth` 防歧义）。
> 3. 空间表：密码 ∈ `[a-zA-Z0-9_.-]{3,64}` 且非保留前缀/字面量；机器 ∈ `apikey-*`；OAuth ∈ `oauth-*`；系统 ∈ `{anonymous, api_user}`——**四空间两两不相交**。
> 4. 存量 `apikey-*`/`oauth-*` 密码用户：grandfather（§5.4），碰撞需预知机器 user_id（128-bit 随机），风险可接受。

#### 5.1.3 `ValidateWorkspaceWorkDir` 签名重构（H1）

```go
// ValidateWorkspaceWorkDir 校验 dir 恰好等于或位于 sandboxRoot 下。
// 这是 workspace 专用的额外约束，不替代通用 ValidateWorkDir（黑名单/symlink 仍需独立调用）。
// dir 必须已是绝对路径（调用前先经 config.ExpandAndAbs）。
// sandboxRoot 由调用方经 WorkspaceSandboxRoot(...) 组装。
func ValidateWorkspaceWorkDir(dir, sandboxRoot string) error {
	rel, err := filepath.Rel(sandboxRoot, dir)
	if err != nil {
		return ErrWorkDirOutsideSandbox
	}
	if rel == "." { // dir == base 合法
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrWorkDirOutsideSandbox
	}
	return nil
}
```

- 原可测试内核 `validateWorkspaceWorkDir(dir, home, ownerUserID)` 并入新签名（参数简化为 sandboxRoot）；`security_test.go:190-243` 既有用例按新签名更新（断言语义不变）。
- `os.UserHomeDir()` 错误分支删除——`HotplexHome()` 内部已处理回退。

#### 5.1.4 handler 组装 sandboxRoot（`workspace_handlers.go`，P4）

```go
// sandboxRootFor 解析 uid 对应的沙箱根。
// 认证层已保证：非系统身份（非 anonymous/api_user）Lookup 必然成功（auth.go:207-215
// 失败即 401）；系统身份与 dev 模式（idp nil）直接以 uid 字面量作为目录段
// （与 v1.40 现状一致——anonymous/api_user 共享 uid 沙箱）。无 403 失败路径。
func (h *WorkspaceHandlers) sandboxRootFor(r *http.Request, uid string) string {
	name := uid // fallback：dev/anonymous/api_user/防御性 Lookup 失败 → uid 字面量
	if idp := h.auth.IdentityProvider(); idp != nil && uid != "anonymous" && uid != "api_user" {
		if u, err := idp.Lookup(r.Context(), uid); err == nil && u.Username != "" {
			name = u.Username
		}
	}
	return security.WorkspaceSandboxRoot(name)
}
```

- **Create**（`:123`）：`root := h.sandboxRootFor(r, uid)`；`ValidateWorkspaceWorkDir(abs, root)`。
- **Update**（`:236`）：`root := h.sandboxRootFor(r, ws.OwnerUserID)`（admin 代改时按 **owner**，保持 owner 隔离，语义同现状 `:234-235` 注释）。
- 错误消息动态化（H2/H3）：`"work_dir must be under " + root`。

#### 5.1.5 List 响应携带 `workspace_root`（`workspace_handlers.go:156`）

```go
respondJSON(w, map[string]any{
	"workspaces":     wss,
	"workspace_root": h.sandboxRootFor(r, uid), // 绝对路径，服务端展开（复用 helper，避免二次 Lookup）
	"limit":          limit,
	"offset":         offset,
})
```

### 5.2 前端：动态 workspace 根（H4/H5）

#### 5.2.1 `webchat/lib/utils/workspace-path.ts`

```ts
// workspaceSandboxPrefix 不再硬编码 ~/.hotplex；root 由服务端 List 响应提供（绝对路径，已含段名）。
export function workspaceSandboxPrefix(root: string): string {
  return `${root.endsWith('/') ? root : root + '/'}`;
}

// buildWorkspaceWorkDir(root, name, subdir?)：root 为服务端 workspace_root
export function buildWorkspaceWorkDir(root: string, name: string, subdir?: string): string {
  const seg = subdir && subdir.trim() ? sanitizeWorkspaceDirMulti(subdir) : sanitizeWorkspaceDir(name);
  return workspaceSandboxPrefix(root) + seg;
}

// subdir 多级支持：按 / 分段 sanitize（G2），每段沿用 [a-z0-9-] 规则
export function sanitizeWorkspaceDirMulti(rel: string): string {
  return rel.split('/').map(sanitizeWorkspaceDir).filter(Boolean).join('/') || 'workspace';
}

// resolveSandboxAnchor(workDir)：从 work_dir 自提取 workspaces/<segment>/ 锚点（v3 修正）。
// 无需身份参数——兼容 UUID grandfather 根与 username 根；命中沙箱根本身时 seg=''。
// 不命中 workspaces/ 结构 → null，调用方禁用编辑并告警（现状语义保留）。
export function resolveSandboxAnchor(workDir: string): { prefix: string; seg: string } | null {
  const anchor = 'workspaces/';
  const idx = workDir.lastIndexOf(anchor);   // 取最后一段 workspaces/，避免用户名/路径前缀误匹配
  if (idx < 0) return null;
  const after = workDir.slice(idx + anchor.length);
  const segEnd = after.indexOf('/');
  if (segEnd < 0) return { prefix: workDir, seg: '' };              // 沙箱根本身
  const seg = after.slice(0, segEnd);
  return { prefix: workDir.slice(0, idx + anchor.length + seg.length + 1), seg: after.slice(segEnd + 1) };
}
```

- 兼容降级：若 `root` 未知（理论不可达，创建入口均在列表加载后），表单禁用提交并提示刷新。

#### 5.2.2 `webchat/lib/api/workspaces.ts`

`ListWorkspacesResponse` 增加 `workspace_root: string`，`listWorkspaces()` 透传。

#### 5.2.3 `NewWorkspaceForm`（H5 + G3）

- Props 增加 `workspaceRoot: string`（父组件由 `listWorkspaces()` 结果提供）。
- 占位符：`{workspaceRoot}/…`；预览：`buildWorkspaceWorkDir(workspaceRoot, name, subdir)`。
- subdir 输入 placeholder 提示支持多级（如 `projects/myapp`）。
- admin 时显示 `permission_mode` 下拉（`read-only | workspace | auto-edit | bypass`），非 admin 不渲染。

#### 5.2.4 调用点同步（完整清单）

| # | 调用点 | 改动 |
|---|--------|------|
| F1 | `ChatContainer.assistant-ui.tsx:249-263`（`loadWorkspaces`） | 保存 `res.workspace_root`（当前丢弃）；自动创建改 `buildWorkspaceWorkDir(wsRoot, "Default Workspace", "default")`（v3 修正：name 非 me.id） |
| F2 | `ChatContainer.assistant-ui.tsx:797`（`NewWorkspaceModal`） | 透传 `workspaceRoot` prop |
| F3 | `NewWorkspaceModal.tsx:7-43`（新增 prop 转发） | `workspaceRoot` 透传给 `NewWorkspaceForm` |
| F4 | `NewWorkspaceForm.tsx:29-38` | `buildWorkspaceWorkDir(root, name, subdir)`；`:31` 占位符动态化 |
| F5 | `settings-modal/general-tab.tsx:44,66` | `resolveSandboxAnchor(workspace.work_dir)`（去掉 owner_user_id 参数）；段可编辑性判断改为：锚点 null 或 work_dir 不在当前 `workspace_root` 前缀下 → 只读（UUID grandfather 根兼容） |

### 5.3 数据流

```
ChatContainer 初始化
  → listWorkspaces() → { workspaces, workspace_root: /data/hotplex/workspaces/alice }
  → NewWorkspaceForm(workspaceRoot=workspace_root)
      → createWorkspace(name, buildWorkspaceWorkDir(root, name, subdir))
      → POST /api/workspaces { name, work_dir: "/data/hotplex/workspaces/alice/<seg>" }
  → handler.Create
      → config.ExpandAndAbs        // 已是绝对路径，幂等
      → security.ValidateWorkDir   // 黑名单 + symlink（通用）
      → sandboxRootFor(uid)        // 系统身份 fallback（P4）或 username 映射（§5.1.2）
      → security.ValidateWorkspaceWorkDir(abs, root)
      → store.CreateWorkspace      // work_dir 唯一约束
  ← Workspace JSON（含 work_dir 绝对路径）
```

### 5.4 兼容性与迁移

| 场景 | 行为 |
|------|------|
| `HOTPLEX_HOME` 未设置 | 沙箱根 = `~/.hotplex/workspaces/<segment>`；**段名从 UUID 变为 username 派生是有意变更**（G1a），文件位置/可用性语义一致 |
| **存量 UUID 根（P3）** | DB 存绝对路径，不重新校验，继续可用（grandfather）；该 workspace 的 work_dir **不可再改**（按新 base 校验 403），name/overrides 等字段不受影响（评审 R5：Update 将未变更 work_dir 视为 no-op，跳过沙箱重校验——前端 general-tab 保存时总是携带未变 work_dir，若不跳过则存量 workspace 的一切保存都会 403）；前端按 §5.2.4 F5 显示只读 |
| **存量 `apikey-*`/`oauth-*` 密码用户** | username 原样作目录段（grandfather）；碰撞需预知机器 user_id（128-bit 随机），风险可接受；新注册已封锁 |
| 设置后新建 workspace | 落于 `$HOTPLEX_HOME/workspaces/<segment>/<seg>` |
| 设置 → 取消 `HOTPLEX_HOME` | 历史 workspace 仍指向旧绝对路径，正常 |
| 新前端 + 旧后端 | `workspace_root` 缺失（undefined）→ 表单降级禁用 + 提示升级后端；不发送错误路径 |
| 旧前端 + 新后端 | 硬编码 `~/.hotplex/...` 在 `HOTPLEX_HOME` 部署下 403（预期），SDK 文档/CHANGELOG 注明 |
| username 改名（P2 未来场景） | 冻结"username 不可变"；若未来引入改名，旧根 workspace 走 grandfather，新 workspace 落新根 |
| `HOTPLEX_HOME` 为相对路径 | `WorkspaceSandboxRoot` 内部 `filepath.Abs` 兜底（§5.1.1），root 恒为绝对路径 |

### 5.5 错误码

| HTTP | code | 触发 | 变化 |
|------|------|------|------|
| 403 | `WORK_DIR_OUTSIDE_SANDBOX` | 越出 `WorkspaceSandboxRoot(segment)` | 语义不变，base 动态化；消息内容变化（更准确） |

无新增错误码；`extractApiError` 透传机制不变。（v2 的"uid→username 解析失败 403 INVALID_CREDENTIALS"**已移除**——认证层保证解析成功，P4。）

### 5.6 身份与安全边界（P1/P2/P4 汇总）

- **四空间隔离 + 空间内注入**：密码/OAuth/机器/系统两两不相交（§5.1.2 + P1 保留清单），同空间内 sanitize 有损/大小写差异经稳定哈希消除碰撞（G1b），目录段碰撞在静态层面杜绝，零 TOCTOU。
- **filesystem-safe**：所有非 `[a-zA-Z0-9_.-]` 字符统一 sanitize（含 `/`、`..`、Windows 保留字符），杜绝路径嵌套/逃逸。
- **owner 隔离不变**：`filepath.Rel` 前缀校验，`..` 越界拒绝。
- **系统身份共享沙箱（现状语义，非本规格引入）**：`anonymous`（dev）与 `api_user`（未映射 API key）各自共享 uid 沙箱；修复（每 key 独立映射）属独立设计，见 §9。

### 5.7 顺带修正（P2，独立提交，不阻塞本规格）

- H7 `gateway_run.go:1655`：skills 目录 → `filepath.Join(config.HotplexHome(), "skills")`
- H8 `messaging_init.go:255`：phrases 目录 → `filepath.Join(config.HotplexHome(), "phrases")`

---

## 6. 测试

### 6.1 后端单测

| 用例 | 内容 |
|------|------|
| `TestWorkspaceSandboxRoot`（新增） | ① `t.Setenv("HOTPLEX_HOME", "/x")` → `/x/workspaces/alice`；② 未设置 → `$HOME/.hotplex/workspaces/alice`；③ `HOTPLEX_HOME` 相对值（`t.Setenv("HOTPLEX_HOME", "rel")`）→ 返回绝对路径（Abs 兜底）；④ `apikey:<id>` → `/…/workspaces/apikey-<sanitized id>`；⑤ `github:user` → `/…/workspaces/oauth-github-user-<64-hex SHA-256>`（有损 → 完整摘要后缀） |
| `TestSandboxDirSegment`（新增，四空间表驱动 + 注入性） | 密码 `alice`/`alice-smith` 原样；系统 `anonymous`/`api_user` 原样；机器 `apikey:abc` → `apikey-abc`、`apikey:a/b` → `apikey-a-b-c14cddc0…`（有损 + 完整摘要）、`apikey:..` → `apikey-5ec1f7e7…`（逃逸段退化为纯摘要）、空输入 → 纯摘要段；OAuth `github:user` → `oauth-github-user-e14ec476…`、`github:user/1` → `oauth-github-user-1-b2c25428…`；`TestSandboxDirSegment_Injectivity`：`apikey:a/b` vs `apikey:a-b`、`github:user/1` vs `github:user-1`、`Alice` vs `alice` 等碰撞对必须映射不同段（G1b）；**R6 跨分支碰撞对**：`apikey:Abc` vs `apikey:abc-<sha256(Abc)>`、`!!!` vs `<sha256(!!!)>`（digest 输出形态的恒等输入回放） |
| `TestValidateWorkspaceWorkDir`（重构） | 新签名（sandboxRoot）：root 本身/`root/sub`/`root/a/b` 通过；`root/../other`、`/tmp/x`、其他用户 root 拒绝；`HOTPLEX_HOME` 注入用例：dir 在 `tmp/workspaces/<segment>` 通过、在 `$HOME/.hotplex/workspaces/<segment>` 拒绝（**证明沙箱不再锚定真实 home**） |
| `TestValidateUsername_NamespaceReserved`（新增，P1） | `apikey-<x>`、`oauth-<x>` 前缀拒绝；字面量 `anonymous`、`api_user` 拒绝；`apikey:<x>` 拒绝（既有）；`alice`/`alice-smith` 通过；回归 `identity_provider_test.go` 既有表驱动用例 |
| `TestOAuthProviderNameReserved`（新增，P1） | OAuth provider 配置校验：`apikey`、`oauth` 拒绝，`github` 通过（校验函数所在包测试） |

> 串行执行（`t.Setenv` 影响进程级环境，遵循 `config_test.go` `TestNormalizePath` 惯例，不加 `t.Parallel()`；与包内既有并行测试并存时依赖 Go 测试框架的串行用例语义，必要时独立文件隔离）。

### 6.2 后端集成（`workspace_handlers_test.go`）

- 辅助函数（H6）：改用 `security.WorkspaceSandboxRoot(username)` 构造沙箱内路径。
- 新增 `TestWorkspace_List_ReturnsWorkspaceRoot`：`HOTPLEX_HOME` 注入 → List 响应 `workspace_root` == `tmp/workspaces/<segment>`。
- 新增 `TestWorkspace_Create_SystemIdentityFallback`：dev 模式（idp nil，uid=`anonymous`）创建 workspace → 成功，work_dir 落 `workspaces/anonymous/` 下（**P4 回归，v2 会 403**）。
- 新增 `TestWorkspace_Update_AdminEditsOtherOwner`：admin 代改他人 workspace 的 work_dir → 按 **owner** 的段校验（owner 段内通过，owner 段外 403）。
- 新增 `TestWorkspace_Update_LegacyUidRoot_UnchangedWorkDirNoop`（R5）：存量 uid-keyed 根 workspace，PATCH 携带未变 legacy work_dir + name 变更 → 200（name 生效、work_dir 原样）；显式修改 legacy work_dir → 403 `WORK_DIR_OUTSIDE_SANDBOX`（P3 不可再改）。
- 既有 `InsideSandbox_Accepted` / `OutsideSandbox_Rejected`：断言消息改为动态匹配或仅断言 code；helper 同步段名化。

### 6.3 前端

- `workspace-path.test.ts`（新增或扩展）：`buildWorkspaceWorkDir(root, name, subdir)` 多级段（`projects/myapp` → `root/projects/myapp`）；`sanitizeWorkspaceDirMulti` 折叠/空段；`resolveSandboxAnchor(workDir)`：username 根（`/…/workspaces/alice/proj` → `{prefix: '/…/workspaces/alice/', seg: 'proj'}`）、UUID 根（`/…/workspaces/<uuid>/proj`）、沙箱根本身（seg=''）、不含 `workspaces/`（null）、路径中含 `workspaces/` 前缀干扰（取最后一段）。
- `pnpm build`（tsc）通过。
- 手动 E2E：见 §8。

---

## 7. 影响范围

- **webchat 用户**：路径预览显示真实根（含本人段名）；`HOTPLEX_HOME` 部署下 workspace 与默认会话同根；目录可读（`ls ~/.hotplex/workspaces/alice/`）。
- **SDK / WS 消费者**：POST/PATCH 契约不变；List 响应新增 `workspace_root`（向后兼容）；硬编码 `~/.hotplex/workspaces/` 构造者在 `HOTPLEX_HOME` 部署下 403 —— CHANGELOG 注明。
- **OAuth 用户**：沙箱段变为 `oauth-<provider>-<subject>`（v1.40 为 UUID 段）；subject 含非法字符/大写时追加完整 SHA-256 摘要（如 `oauth-github-user-1-b2c25428…`）；新部署即生效，存量 UUID 根 grandfather。
- **API-key 机器用户**：沙箱段变为 `apikey-<user_id>`；未映射 key（`api_user`）共享段维持现状（§5.6）。
- **dev 模式（anonymous）**：行为不变（共享 `workspaces/anonymous` 段），P4 回归保证。
- **消息平台（飞书/Slack）**：不涉及（PlatformSession 跳过 workspace 层）。
- **存量部署**：零数据迁移；UUID 根与存量 `apikey-*`/`oauth-*` 密码用户 grandfather（§5.4）。

---

## 8. 验收方案（DoD）

1. **单测全绿**：
   ```bash
   go test ./internal/security/ ./internal/gateway/ -count=1 -race -shuffle=on
   ```
   重点：`TestWorkspaceSandboxRoot`、`TestSandboxDirSegment`、`TestValidateWorkspaceWorkDir`、`TestValidateUsername_NamespaceReserved`、`TestOAuthProviderNameReserved`、`TestWorkspace_List_ReturnsWorkspaceRoot`、`TestWorkspace_Create_SystemIdentityFallback`、`TestWorkspace_Update_AdminEditsOtherOwner`。
2. **质量门禁**：`make lint` 全绿；`go build ./...` 零错误；`cd webchat && pnpm build` 通过。
3. **手动 E2E（HOTPLEX_HOME 部署）**：
   ```bash
   export HOTPLEX_HOME=/tmp/hx-ws-e2e
   hotplex gateway start -d          # 配置同前置 plan 验收 3（测试端口 + admin token）
   cd webchat && pnpm dev            # 或同源嵌入模式，访问 webchat
   ```
   断言：登录（username=alice）→ 新建 workspace "proj" 成功；`work_dir` == `/tmp/hx-ws-e2e/workspaces/alice/proj`；会话在该目录下启动；`/settings` 列表显示正确绝对路径；路径预览不含 `~/.hotplex` 字样。
4. **手动 E2E（未设置 HOTPLEX_HOME 回归）**：unset 后同上 → `work_dir` == `$HOME/.hotplex/workspaces/alice/proj`；若环境存在 UUID 根存量 workspace，验证其可用且段编辑只读（F5）。
5. **联动回归**：设置 `HOTPLEX_HOME` 后 Slack/Feishu 默认目录（`$HOTPLEX_HOME/workspace`）与 webchat workspace 同根（目视验证目录树）。
6. **P1 验证**：`hotplex admin create --username apikey-e2e` 被拒；OAuth provider 名为 `apikey` 的配置被校验拒绝。
7. **兼容矩阵**：新前端 + 旧后端（无 `workspace_root`）→ 表单降级禁用 + 提示；旧前端 + 新后端（`HOTPLEX_HOME` 设置）→ 创建 403（预期，CHANGELOG 已注明）。

---

## 9. 开放问题

- **系统身份共享沙箱（现状弱点，非本规格引入）**：`api_user` 是所有未映射 API key 的共享段，`anonymous` 是 dev 共享段。修复（每 key 独立映射 / dev 按用户隔离）需独立设计 + 迁移，列入后续 spec。
- **机器用户目录段可读性**：`apikey-<user_id>` 仍不可读；可接受（机器用户无排障归属诉求）。
- **per-user 沙箱根自定义**（未来增强）：统一根保 owner 隔离最简；放开需用户级配置项 + 迁移策略，后续 spec。
- **`workspace_root` admin 视图（跨用户）**：admin 编辑他人 workspace 的提示已按 owner 计算（§5.1.4），UI 是否展示区分，实现时确认。
- **前端 root 缓存失效**：`workspace_root` 与 gateway 进程同生命周期（HotplexHome 静态），无需轮询；运行期变更需刷新机制——当前不处理。
- **username 改名（若未来引入）**：P2 冻结语义被打破时需"身份锚点迁移"流程（旧根 grandfather + 新根接管 + 审计），本规格不实现。

---

## 10. 评审修订记录

| 轮次 | 评审方 | 发现 | 处置 |
|------|--------|------|------|
| 1 | 用户评审 | 沙箱末段用 UUID 用户体验不佳，建议 username | 采纳（v2）：目录段改 username；补 P1（`apikey-` 保留前缀）、P2（不可变冻结）、P3（UUID 根 grandfather） |
| 2 | Momus 审查（v2） | **P1**：`sandboxRootFor` 对 dev-mode anonymous / 未映射 api_user 会 403（认证层跳过 Lookup，`auth.go:207,263`），破坏现状 | v3 修正（P4）：系统身份/idp nil → 目录段 = uid 字面量，无 403 路径；§6.2 移除不可达的"用户不存在→403"用例，改系统身份 fallback 集成测试 |
| 2 | Momus 审查（v2） | **P1**：`:`→`-` 变换非单射——OAuth `provider:subject`（`oauth_handlers.go:215`，绕过 ValidateUsername）可碰撞机器 `apikey:<uuid>`；API key user_id 可含路径分隔符；§5.1.1/§5.1.4/§6 引用不存在的 `security.SandboxDirSegment` | v3 修正：四身份空间隔离编码（§5.1.2）；P1 保留清单扩展（`apikey-`/`oauth-` 前缀 + 字面量 + OAuth provider 名校验）；`WorkspaceSandboxRoot` 导出唯一 API 内部执行映射 |
| 2 | Momus 审查（v2） | **P2**：普通 List 无 `owner_username`，`resolveSandboxAnchor(workDir, username)` 数据依赖不闭合；ChatContainer 丢弃 `workspace_root`；NewWorkspaceModal prop 传递未列；G4"与现状一致"表述不准；E2E 未覆盖新前端连旧后端等 | v3 修正：锚点改 work_dir 自提取（无身份参数，兼容 UUID 根）；调用点完整清单（F1-F5）；G4 文案修正；验收补兼容矩阵 |
| 2 | Momus 审查（v2） | **P2**：`HotplexHome()` 对 `HOTPLEX_HOME` 原样返回（`config.go:172-173`），相对值导致 root 非绝对 | v3 修正：`WorkspaceSandboxRoot` 内部 `filepath.Abs` 兜底 + 测试用例 |
| 3 | 实现后评审（F1-F6） | **F1（P1）**：sanitize 有损导致**同空间内**段碰撞（OAuth `user/1` vs `user-1`、大小写不敏感文件系统 `Alice` vs `alice`），违反 G1b | **用户确认采纳**：`lossySafeSegment` 有损/含大写时追加完整 SHA-256 摘要（§5.1.2 修订；评审第二轮否决 4-byte 截断——已构造出实际碰撞对）；新增 `TestSandboxDirSegment_Injectivity`；§6.1 表驱动预期同步（含确定性摘要值） |
| 3 | 实现后评审 | **F2（P1）**：无冒号分支原样返回，防御性 fallback uid（含 `/`、`..`）可逃出 `HotplexHome()/workspaces` | `lossySafeSegment` 应用于无冒号分支（恒等输入不受影响；病理 uid 被 sanitize + 哈希收敛为单段） |
| 3 | 实现后评审 | **F3（P1）**：work_dir 恰好等于沙箱根时，Settings 段编辑重拼出 `aliceproj`（缺分隔符） | 新增 `rejoinWorkDir(prefix, seg)`：空段返回去尾斜杠根、非空段保证恰一个分隔符（§5.2.4 F5） |
| 3 | 实现后评审 | **F4（P1）**：多级 work_dir（`projects/myapp`）在 Settings 保存时被单段 sanitize 拍平为 `projects-myapp`（改 name 也会误迁目录） | `rejoinWorkDir` 用 `sanitizeWorkspaceDirMulti` 保留多级段 + 预览同步修复 |
| 3 | 实现后评审 | **F5（P1）**：系统身份占位行（`password_hash=''`）可被 admin 密码重置接口改造成可登录账号，接管共享沙箱 | `ResetUserPassword` 拒绝空哈希用户（`PASSWORD_RESET_BLOCKED` 403），正常密码用户不受影响 |
| 3 | 实现后评审 | **F6（P1）**：存量 `anonymous`/`api_user` 字面量用户名（P1 前注册）与占位行 username 唯一冲突被吞后 FK 仍失败（500） | `ensureSystemIdentityRow` 冲突时核验同 ID 行；存量用户名冲突以 `uid-system-N` 确定性后缀补行（沙箱段仍为 uid 字面量） |
| 3.5 | 实现后评审（第二轮） | **R1（P1）**：4-byte 哈希截断可被暴力构造碰撞对（已给出实际碰撞串），F1 修复仍非注入 | `lossySafeSegment` 改追加**完整 SHA-256 摘要**（64 hex，碰撞等价于 SHA-256 碰撞）；§6.1 预期同步 |
| 3.5 | 实现后评审（第二轮） | **R2（P2）**：空 username 经 `lossySafeSegment("")` 返回空段，`WorkspaceSandboxRoot("")` 落到 `workspaces/` 根 | 恒等判定增加 `s != ""`，空输入退化为纯摘要段；`TestSandboxDirSegment` 补空输入用例 |
| 3.5 | 实现后评审（第二轮） | **R3（P2）**：`uid-system` 重试的第二次唯一冲突被吞掉，FK 仍失败 | 确定性后缀循环（`uid-system-N`，16 次穷尽后显式报错） |
| 3.5 | 实现后评审（第二轮） | **R4（P2）**：spec §6.1 `TestWorkspaceSandboxRoot` 第⑤行仍写无摘要段名，与实现/§6.1 其他行不一致 | 该行同步为"有损 → 完整 SHA-256 摘要后缀" |
| 4 | PR #963 code-review | **R5（P1）**：Update 无条件重校验 work_dir——前端 general-tab 保存总是携带未变 work_dir，存量 UUID 根 workspace 的 name/overrides 等一切保存都 403，违背 P3"其他字段不受影响" | Update 将未变更 work_dir 视为 no-op（跳过沙箱重校验），校验只在值真正变化时生效；`TestWorkspace_Update_LegacyUidRoot_UnchangedWorkDirNoop`（§6.1） |
| 4 | PR #963 code-review | **R6（P1）**：`lossySafeSegment` 跨分支碰撞——digest 输出形态本身是合法恒等输入（`apikey:Abc` → `apikey-abc-<h>` vs 恒等输入 `apikey:abc-<h>`；空 base 变体 `!!!` → `<h>` vs 恒等输入 `<h>`），违反 G1b 注入性 | 恒等分支排除 digest 形态（`-[0-9a-f]{64}$` 后缀 / 纯 64-hex）；`TestSandboxDirSegment_Injectivity` 补两组跨分支碰撞对（§5.1.2/§6.1 同步） |
