# Skill 管理 — HTTP API 与 zip 上传

**状态**: Implemented · **日期**: 2026-07-17 · **跟踪 issue**: #910 · **关联**: [Admin-Workspace-PermissionMode-Management-Spec.md](./Admin-Workspace-PermissionMode-Management-Spec.md)、[ACP-Worker-Spec.md](./ACP-Worker-Spec.md) · **版本目标**: v1.37.0

---

## 1. 背景与动机

当前 skill 列表只有一个入口：WS 的 `worker_cmd`（`StdioSkills`）→ `handleSkillsList`（`internal/gateway/worker_cmds.go:191`）。它**强依赖 session**（需 `si.WorkDir` 解析项目级 skill），且只读、无管理能力。

webchat 要提供 skill 管理 UI，诉求分两类角色：

1. **普通用户**管理**自己 workspace**下的 skill。
2. **管理员**管理**全局** skill 和**管理员自己的** workspace skill，**不管**普通用户的 skill。

同时最初的用户反馈是"给一个独立 HTTP 接口查 skill 列表"——脱离 session、每个用户看到 `全局 + 自己 workspace`。

### 1.1 现状盘点（调研确认）

- **`internal/skills/`** 是纯只读扫描器：`Locator.List` + `ParseFrontmatter`；`Skill{Name,Description,Source}` **无 body、无 path**。
- **Locator 缓存**：key 仅 `workDir`、TTL 5min、**无 Invalidate 方法**。
- **Scanner** 扫 5 个目录：home 下 `.claude`/`.agents`/`.hotplex`，workDir 下 `.claude`/`.agents`；dedup 规则"同名 project 覆盖 global"（`scanner.go:191`）。
- **Worker 加载**：hotplex 代码**不向任何 worker 传 skill 目录参数**（各 worker 启动函数无 skill 参数/env），完全靠 worker 原生 home 路径解析。
- **无 multipart 上传**：全仓零命中 `ParseMultipartForm`/`FormFile`/`MaxBytesReader`（排除 docs/tests），所有写 handler 走 JSON + `io.LimitReader(r.Body, 1<<20)`。
- **无通用 unzip 工具**：`updater.extractFromZip` 是单文件提取器，不可复用。

### 1.2 Worker 加载约定（决定 onboard 章节）

| Worker | 原生读 `~/.agents/skills` | 项目级路径 | 所需操作 |
|---|---|---|---|
| Claude Code | ❌ 只读 `.claude/skills` | `<project>/.claude/skills` | **必须软链** `~/.claude/skills → ~/.agents/skills` |
| Codex CLI | ✅ 主路径就是 `.agents/skills` | `<project>/.agents/skills` | 无需 |
| OpenCode Server | ✅ agent-compat 路径 | `<project>/.agents/skills` | 无需 |
| ACP | 取决于底层 agent | 同底层 | 按底层 agent 处理 |

> 来源：Codex 官方源码 `codex-rs/core-skills/src/loader.rs`（主路径 `$HOME/.agents/skills`）、OpenCode 官方 `skills.mdx`（列 `.agents/skills` 为 agent-compat 扫描路径）、Claude Code 官方文档（只列 `.claude/skills`）。

## 2. 范围（已与 stakeholder 确认）

| 决策点 | 选择 | 理由 |
|---|---|---|
| 统一存储目录 | **`.agents/skills`**（全局 `~/.agents/skills`，workspace `<ws>/.agents/skills`） | 对齐开放 `.agents` 标准；与 workspace 端对称；Codex/OCS 原生读取 |
| 软链管理 | **程序不代建**，onboard 引导 + 手册说明 | 把"已有真实目录覆盖、方向冲突、跨 worker 归一化"等数据安全风险移出程序代码 |
| workspace 绑定 | **workspace 唯一绑定物理目录** | 已有 `Workspace.WorkDir`，沙箱校验落到 `$HOME/.hotplex/workspaces/<uid>/` |
| 普通用户管理范围 | **仅自己 workspace** | owner 校验闸 |
| 管理员管理范围 | **全局 + 管理员自己的 workspace** | 不存在第三个存储；角色只决定能否碰全局 |
| 创建/修改方式 | **zip 包上传**，不提供在线编辑器 | 强制走结构化校验；避免自建编辑器 + 保证 skill 标准 |
| 同名上传 | **replace 覆盖** | 改 skill = 重新打包上传 |
| 同名跨范围 | **允许安装但返回 warning** | workspace skill 同名会覆盖全局生效，UI 必须提示 |
| 列表可见性 | **展示 `.agents/skills`（managed 可写）+ `.claude/skills` 等（external 只读）**，带来源标注 | 让列表诚实反映 worker 实际会加载的 skill |
| Worker 兼容性标注 | **不在本项目范围** | skill 跨 worker 兼容由用户自负 |

**明确不做**：
- ❌ 程序代建 worker 软链（onboard 引导即可）
- ❌ skill 在线编辑器（仅 zip 上传 + 删除）
- ❌ skill 的 worker 兼容性 frontmatter（`compatibility` 字段校验忽略）
- ❌ 管理员管理普通用户的 workspace skill（越权）

## 3. 核心改动

### 3.1 存储模型

```
全局（managed，admin 写）:   ~/.agents/skills/<name>/SKILL.md
workspace（managed，owner 写）: <workspaceDir>/.agents/skills/<name>/SKILL.md
```

- `<workspaceDir>` 解析入口：`store.GetWorkspaceByID(ctx, wid).WorkDir`（`internal/session/multitenancy_store.go:359`）。
- 写文件沿用 `security.ValidateWorkspaceWorkDir` 沙箱校验（`workspace_handlers.go:123`），防路径穿越。
- `.claude/skills`、`.hotplex/skills` 等保留为**只读外部源**（列表展示，不写）。

### 3.2 `internal/skills/` 包改造

**(a) Skill 结构体扩展**（`skills.go`）

```go
type Skill struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Source      string `json:"source"`  // 保留 "global"/"project"（= scope）；wire 兼容
    Managed     bool   `json:"managed"` // 新增：是否在 .agents/skills 可写区
    FilePath    string `json:"-"`       // 新增：SKILL.md 绝对路径（CRUD 详情/删除用，不下发 wire）
}
```

> `Source` 语义：`global` = home 下，`project` = workspace/workDir 下（即 scope）。`Managed=true` 表示位于 `.agents/skills`（UI 可改）；`false` 表示外部只读（`.claude/skills` 等用户自有）。wire 仅加 `Managed`（omitempty，向后兼容）。

**(b) Locator 缓存失效**（`locator.go`）

```go
// 新增：失效单 workspace 的缓存
func (l *Locator) Invalidate(workDir string)
// 新增：全局变更影响所有 workspace 列表，兜底全清
func (l *Locator) InvalidateAll()
```

- cache key 当前仅 `workDir`：workspace CRUD 后调 `Invalidate(workDir)`；**全局 CRUD 后必须 `InvalidateAll()`**（全局 skill 变更使所有 workspace 的合并列表失效）。

**(c) CRUD + zip 安装**（新文件 `internal/skills/crud.go`、`internal/skills/zip.go`）

```go
type Scope string
const (
    ScopeGlobal    Scope = "global"    // 写 ~/.agents/skills
    ScopeWorkspace Scope = "workspace" // 写 <workDir>/.agents/skills
)

type Detail struct {
    Skill
    Body     string   `json:"body"`      // SKILL.md 全文
    Files    []string `json:"files"`     // 包内文件相对路径列表
}

// 安装（create/replace 同一入口，replace=true 时覆盖同名）
func (l *Locator) Install(ctx context.Context, scope Scope, baseDir string, zr *zip.Reader, replace bool) (*Detail, error)
func (l *Locator) Read(ctx context.Context, scope Scope, baseDir, name string) (*Detail, error)
func (l *Locator) Delete(ctx context.Context, scope Scope, baseDir, name string) error
```

`baseDir`：global 传 `homeDir`，workspace 传 `workspaceDir`。写后自动调对应 `Invalidate`。

### 3.3 zip 校验管线（`internal/skills/zip.go`，安全敏感）

**A. 安全层（硬红线）**

| 威胁 | 缓解 | 落地 |
|---|---|---|
| Zip-slip（路径穿越） | 复用 `security.SafePathJoin`（`internal/security/path.go:173-207`，已拒 `../`/绝对路径/symlink 逃逸）+ `strings.HasPrefix(dest, baseDir+sep)` 双保险 | 每 entry 校验 |
| 解压炸弹 | 累加阈值，超限拒绝 | zip ≤20MB、解压总 ≤50MB、单文件 ≤5MB（`io.LimitReader`）、entry ≤500、压缩率 >100× 拒、禁嵌套 zip |
| 恶意/非常规 entry | 复用 `updater.go:265` 的 `f.Mode().IsRegular()` 过滤 | 拒 symlink/device/pipe entry |

**B. 格式/规范层**（对齐 [agentskills.io](https://agentskills.io/specification) 标准）

1. **结构**：zip 根下要么直接有 `SKILL.md`，要么只有一个顶层目录、其内有 `SKILL.md`。
2. **frontmatter 必填**：`name` + `description`（`extractFrontmatter`，`scanner.go:164`）。
3. **`name` 校验**：正则 `^[a-z0-9]+(-[a-z0-9]+)*$`、1-64 字符、**必须等于父目录名**。
4. **`description`**：1-1024 字符（**独立严格校验**；不复用 `ParseFrontmatter` 的 120-rune 展示截断——那是 UI 用，不是标准）。
5. **scope 唯一性**：create 时目标 scope 同名且 `replace=false` → 拒绝。
6. **跨 scope 同名 warning**：workspace 安装撞全局 → **允许安装**，返回 `warning: "shadows global skill '<name>'"`，UI 提示。
7. **文件类型白名单**：`.md`/`.json`/`.yaml`/`.yml`/`.txt`/`.py`/`.sh`/`.toml`/图片（`.png`/`.jpg`/`.svg`）；拒可执行/二进制。

**C. 落盘**：解压到 temp 目录 → 全部校验通过 → 原子 `os.Rename` 到 `<baseDir>/.agents/skills/<name>/` → `Invalidate`。任一步失败回滚，不留半成品。

### 3.4 HTTP API

```
# 所有人（核心查询，闭合最初需求）
GET    /api/skills                         # 合并列表：global + 我的workspace + 外部只读，带 Managed 标注
GET    /api/skills/{name}                  # 合并详情（取覆盖胜出的 scope）

# workspace owner（含管理员的 own workspace）
POST   /api/workspaces/{wid}/skills        # zip 上传安装（multipart，?replace=true）
GET    /api/workspaces/{wid}/skills/{name}
DELETE /api/workspaces/{wid}/skills/{name}

# 管理员（全局）
GET    /admin/api/skills
POST   /admin/api/skills                   # zip 上传安装（multipart，?replace=true）
GET    /admin/api/skills/{name}
DELETE /admin/api/skills/{name}
```

**鉴权**（复用现成）：
- 用户端：`auth.AuthenticateRequest(r)` → uid（`internal/security/auth.go:155`，API key + cookie fallback）；workspace 归属校验 `ws.OwnerUserID != uid && !isAdmin` → 403（`workspace_handlers.go:174`）。
- 管理员端：`AdminAPI.Middleware` Bearer + scope（`internal/admin/admin.go:198`）；`admin:write` 蕵含 `admin:read`（`middleware.go:22-27`）；写操作 `requireScope(w, r, admin.ScopeAdminWrite)`。

**multipart 上传**（新建，handler 层）：
- `http.MaxBytesReader(w, r.Body, 20<<20)` 包 body（20MB 上限）。
- `r.ParseMultipartForm(20 << 20)` 或 `r.MultipartReader()` 流式取 `file` 字段。
- 读取后交给 `skills.Install(ctx, scope, baseDir, zipReader, replace)`。

**路由注册**（`cmd/hotplex/routes.go`，Go 1.22 ServeMux，无 group）：
```go
// 用户 workspace skill 路由（套 workspace 块同款 corsMw(csrfMw(...))）
mux.Handle("POST /api/workspaces/{wid}/skills", corsMw(csrfMw(http.HandlerFunc(skillHandlers.InstallWorkspace))))
// admin 全局 skill 路由（套 adminAPI.Middleware；写操作加 AuditWrite）
mux.Handle("POST /admin/api/skills", corsMw(userAdmin.AuditWrite(admin.AuditSkillCreate, csrfMw(http.HandlerFunc(skillHandlers.InstallGlobal)))))
```
每条写路由配套 `OPTIONS` preflight；`{wid}`/`{name}` 用 `r.PathValue`。

### 3.5 审计接入（`internal/admin/audit.go`）

新增动作枚举：
```go
AuditSkillCreate = "skill.create"
AuditSkillUpdate = "skill.update"   // replace 覆盖
AuditSkillDelete = "skill.delete"
```
全局写操作（`POST/DELETE /admin/api/skills*`）经 `userAdmin.AuditWrite(action, handler)` 统一记录 `admin_audit` slog（`actor`=uid/admin-token，`target`=路径，`result`=ok/failed）。**workspace 端点（用户自助）** 写操作由各 handler 成功路径显式调 `admin.AdminAudit`（与现有 `/api/admin/*` 写操作一致）。读操作不审计。

### 3.6 前端

- `webchat/app/admin/skills/page.tsx`：管理员全局 skill 管理页（列表 + 上传 + 删除）。
- `webchat/app/.../workspace-skills`：普通用户 workspace skill 管理页（同结构，scope=workspace）。
- 上传组件：`<input type="file" accept=".zip">` → POST multipart；展示 warning toast（同名覆盖/撞全局）。
- 列表按 `managed` 标注"可管理 / 只读外部"。
- admin-nav 加 `Skills` 入口。

## 4. 安全考量

| 威胁 | 缓解 |
|---|---|
| Zip-slip 逃出 skills 目录 | `security.SafePathJoin` + `HasPrefix` 双保险（§3.3 A） |
| 解压炸弹 DoS | 大小/数量/压缩率多维阈值（§3.3 A） |
| zip 内恶意 symlink entry | `f.Mode().IsRegular()` 过滤 |
| 恶意文件类型（可执行/二进制） | 扩展名白名单 |
| 非 owner 越权写 workspace skill | `ws.OwnerUserID != uid && !isAdmin` → 403 |
| 非 admin 越权写全局 skill | `AdminAPI.Middleware` + `requireScope(admin:write)` |
| skill name 注入 `../` 穿越目录 | name 正则 `^[a-z0-9]+(-[a-z0-9]+)*$` + 必须等于父目录名 |
| 软链默认不生效致"看得见跑不动" | UI 标注加载状态 + onboard 把软链设置做成显式步骤（CC 专属） |
| 全局 CRUD 后列表缓存陈旧 | 全局写后 `Locator.InvalidateAll()` |

## 5. 数据契约汇总

| 端点 | 方法 | 鉴权 | 用途 |
|---|---|---|---|
| `/api/skills` | GET | 用户（Authenticator） | 合并列表：global + 我的 workspace + 外部只读 |
| `/api/skills/{name}` | GET | 用户 | 合并详情 |
| `/api/workspaces/{wid}/skills` | POST | 用户（owner） | zip 安装 workspace skill |
| `/api/workspaces/{wid}/skills/{name}` | GET / DELETE | 用户（owner） | 详情 / 删除 |
| `/admin/api/skills` | GET / POST | admin（admin:read / admin:write） | 全局列表 / zip 安装 |
| `/admin/api/skills/{name}` | GET / DELETE | admin（admin:read / admin:write） | 详情 / 删除 |

错误码：
- `400 SKILL_INVALID_ZIP` — zip 损坏/超限/含恶意 entry
- `400 SKILL_INVALID_FORMAT` — 无 SKILL.md / frontmatter 缺失 / name 不合规 / name≠目录名
- `400 SKILL_FILE_TYPE_BLOCKED` — 文件类型不在白名单
- `409 SKILL_ALREADY_EXISTS` — 同 scope 同名且未带 `?replace=true`
- `404 SKILL_NOT_FOUND` — name 不存在
- `403 PERMISSION_DENIED` — 非 owner / 非 admin

## 6. 测试

**后端**（`internal/skills/zip_test.go`、`crud_test.go` + handler test）：
- zip 安装：合法结构（扁平 SKILL.md / 单顶层目录）成功；zip-slip entry 拒；炸弹超限拒；symlink entry 拒；文件类型黑名单拒；name 正则违规拒；name≠目录名拒；description >1024 拒；replace=true 覆盖；replace=false 同名 409；workspace 撞全局返回 warning。
- CRUD：Install/Read/Delete；写后 `Invalidate` 生效（列表立刻反映）；全局写触发 `InvalidateAll`。
- handler：非 owner 403；非 admin 403；multipart 解析；`MaxBytesReader` 超限 400；审计写入。
- scanner：`Managed`/`FilePath` 正确标注；`.agents/skills` 与 `.claude/skills` 同时出现时 provenance 正确。

**前端**：上传流程（含 warning toast）、删除确认、只读外部 skill 不显示写操作、admin-nav 入口。

**onboard 实测**（实施前）：4 个 adapter 各验证一次——zip 装 skill 到 `~/.agents/skills` 后，CC（建软链后）/Codex/OCS 是否真的在会话中加载；ACP 按底层 agent 验证。

## 7. 文档

- `docs/reference/admin-api.md`：补 `/admin/api/skills*` 契约。
- `docs/reference/api.md`（或对应 webchat API 文档）：补 `/api/skills*` 与 `/api/workspaces/{wid}/skills*` 契约。
- **新增 onboard 手册页**：skill 软链引导（CC 专属命令 `ln -s ~/.agents/skills ~/.claude/skills`；Codex/OCS 无需；ACP 按底层）。
- 本 spec 实施后状态改 `Implemented`，关联 PR。

## 8. 实施清单（分阶段）

**阶段 0 — 实测验证（动工前）**
- [ ] 4 个 adapter 实测 `.agents/skills` 加载行为 + 软链穿透（确认 §1.2 表）

**阶段 1 — skills 包改造（纯后端，无 API）**
- [ ] `internal/skills/skills.go`：`Skill` 加 `Managed`/`FilePath`
- [ ] `internal/skills/scanner.go`：`parseSkillFile` 回填 `FilePath`；区分 managed(`.agents`) vs external(`.claude`/`.hotplex`)
- [ ] `internal/skills/locator.go`：`Invalidate(workDir)` + `InvalidateAll()`
- [ ] `internal/skills/zip.go`：zip 解压 + 安全层 + 格式层校验
- [ ] `internal/skills/crud.go`：`Install/Read/Delete`（scope 参数化）
- [ ] 单元测试 + `make check`

**阶段 2 — HTTP API**
- [ ] `internal/admin/audit.go`：`AuditSkillCreate/Update/Delete` 常量
- [ ] `internal/admin/skill_handlers.go`（或 gateway）：8 个端点 handler + multipart
- [ ] `cmd/hotplex/routes.go`：注册路由 + OPTIONS preflight
- [ ] `pkg/events`：`SkillEntry` 加 `Managed` 字段（wire 兼容性测试）
- [ ] handler 测试 + `make check`

**阶段 3 — 前端**
- [ ] `webchat/lib/api/skills.ts` + 类型
- [ ] `webchat/app/admin/skills/page.tsx` + workspace skill 管理页
- [ ] 上传组件 + warning toast + 只读标注
- [ ] admin-nav 入口
- [ ] `cd webchat && pnpm tsc --noEmit`

**阶段 4 — 文档与 onboard**
- [ ] admin-api.md / api.md 补契约
- [ ] onboard 手册：skill 软链引导页
- [ ] 本 spec 状态 → Implemented

## 9. 待确认项

1. **`Source` 值是否重命名** `project`→`workspace`：是 wire 值变更（客户端 `=="project"` 会断），需协调 SDK；首版**保留** `global`/`project`，仅在文档注明 project=workspace scope。
2. **全局 skill 的物理 owner**：`~/.agents/skills` 在多用户服务器上是 hotplex 服务账号的 home，所有用户共享。确认这是"全局"的预期语义（与本 spec 一致）。

---

## 10. 实施记录

**分支**: `feat/910-skill-management`（关联 issue #910）

### 已实施

- **阶段 1（skills 包）**：`Skill` 加 `Managed`/`FilePath`；scanner 区分 managed(`.agents`)/external(`.claude`/`.hotplex`)；Locator 加 `Invalidate(workDir)`/`InvalidateAll()`/`ListMerged`；`zip.go` 安全管线（zip-slip/炸弹/恶意 entry/类型白名单 + agentskills.io 格式校验）；`crud.go` `Install`/`Read`/`Delete`（原子 Rename 落盘 + 回滚 + 跨 scope warning + 按 scope 缓存失效策略）。
- **阶段 2（HTTP API）**：8 端点（gateway 用户端 5 + admin 4），multipart `MaxBytesReader` 20MB，审计接入（admin `AuditSkillCreate/Update/Delete` + 用户端 `auditCollector` → user_activity），`SkillEntry` wire 加 `Managed`（omitempty 向后兼容）。
- **阶段 3（前端）**：admin 全局 skill 管理页（list + zip 上传 + 删除 + warning toast + 只读外部标注）+ admin-nav `Skills` 入口 + zh-CN/en locales 同步。

### 偏离 spec

- **§3.3 A 复用 SafePathJoin**：`security.SafePathJoin` 对目标路径调 `filepath.EvalSymlinks`，而解压时目标文件尚未创建会直接失败。改用等价的 `safeExtractJoin`（Clean + 拒绝绝对路径/".." + Join + 字符串前缀校验，省去 EvalSymlinks）：staging 为本包新建的受控临时目录、entry 相对路径已严格 Clean，前缀校验足以防 zip-slip；显式 `HasPrefix` 双保险保留。

### 阶段 0（实测验证）

§1.2 的 4 adapter 加载行为已通过**官方源码调研确认**（Codex `loader.rs` 主路径 `$HOME/.agents/skills`、OpenCode `skills.mdx` agent-compat 路径、CC 文档只读 `.claude/skills`）。真实 worker 实测（zip 装 skill 到 `~/.agents/skills` 后 CC 建软链 / Codex / OCS 是否在会话中加载）建议在部署环境验证。

### 待确认项决议

1. `Source` 保留 `global`/`project`（wire 兼容；project=workspace scope，文档注明），重命名留待后续协调 SDK。
2. 全局 `~/.agents/skills` 为服务账号 home、所有用户共享——确认为"全局"预期语义。

### 后续（非本 PR 范围）

- **workspace skill 管理 UI（普通用户页）**：后端 API（`/api/workspaces/{wid}/skills`）+ 前端 client（`webchat/lib/api/skills.ts`）已就绪，UI 待 workspace 详情页改造时接入（复用 admin 全局页模式）。
- **`docs/reference/admin-api.md`、`api.md` 端点契约**：docs/reference 门户待建；本 spec §5 数据契约汇总为权威参考。
- **onboard skill 软链引导**（CC 专属 `ln -s ~/.agents/skills ~/.claude/skills`；Codex/OCS 无需；ACP 按底层）：待 onboard 手册页接入。
