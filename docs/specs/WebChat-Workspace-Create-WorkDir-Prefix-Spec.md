# WebChat Workspace 新建入口 + work_dir 沙箱前缀约束

**状态**: 设计已认可，待实现
**日期**: 2026-06-24
**关联**: `WebChat-Multitenancy-Foundation-Design-Spec.md`、`WebChat-Multitenancy-WorkspaceWorker-Design-Spec.md`、`Turn-Summary-WorkDir-Fix-Spec.md`

---

## 1. 背景

Workspace 是 HotPlex 的**跨通道租户锚**（`internal/gateway/workspace_handlers.go:43` 注释：`cross-channel tenant anchor`）。后端支持单用户拥有**任意多个 workspace**（`Create` 无数量上限，唯一约束是 `work_dir` 不重复 —— `workspace_handlers.go:104` `WORK_DIR_TAKEN`）。Session 的 `work_dir` 从 workspace **继承且不可变**（`api.go:258`），因此 workspace 的 `work_dir` 是整个执行路径的源头。

当前两个问题：

1. **新建入口缺失**：webchat 前端清理了不适用的 `admin/workspaces` 页面体系（多租户管理视角，无主导航入口）后，webchat UI 丢失了手动新建 workspace 的入口。webchat 用户只剩自动创建的 Default Workspace + 切换已有 workspace，无法在 UI 内新建第二个。
2. **work_dir 无前缀约束**：`security.ValidateWorkDir`（`internal/security/path.go:52`）只校验绝对路径 + 黑名单 + symlink，**不强制任何用户级沙箱前缀**。用户（任意通道：REST API / SDK / webchat）可把 workspace `work_dir` 指向任意合法路径，存在越权读写风险。自动创建的 Default Workspace 当前用相对路径 `./workspace`（`ChatContainer:111`），解析为 gateway CWD 下的 `workspace/`，行为不可预测。

## 2. 目标

- **G1 新建入口**：在 webchat UI 提供 workspace 新建入口（两处：`/settings` 完整表单 + ChatContainer 顶部下拉快捷入口），让 webchat 用户能自服务新建 workspace，与后端多 workspace 能力对齐。
- **G2 work_dir 沙箱**：所有**新建/更新**的 workspace `work_dir` 必须落在 `$HOME/.hotplex/workspaces/<owner_user_id>/` 或其子目录下。`$HOME` 为 gateway 运行用户的 home（与现有 `~/.hotplex/` 约定一致），`<owner_user_id>` 为 workspace 归属用户的 user_id（UUID，与 `OwnerUserID` 一致，不可变）。

## 3. 非目标

- **不做数据迁移**：老 workspace 记录的 `work_dir` 原样保留（grandfather）。
- **不改通用 `ValidateWorkDir`**：前缀约束是 workspace 专用语义，不波及 session / worker 的 work_dir 校验。
- **不改 API 契约**：`POST/PATCH /api/workspaces` 仍接收 `{name, work_dir}`，保护 SDK / 其他 WS 消费者。
- **不在本次实现消息平台侧**：飞书/Slack 是 `PlatformSession`，本就跳过 workspace 层（`TestPool_PlatformSessionSkipsWorkspaceLayer`），不受影响也无需改动。

## 4. 决策记录

| 决策点 | 选择 | 理由 |
|---|---|---|
| `<user>` 标识 | `user_id` (UUID) | 与 `workspace.OwnerUserID` 一致，不可变、唯一、无特殊字符问题 |
| 现有 workspace 兼容 | 新建强制前缀 + Default 改前缀 + 老 grandfather | 最小破坏；老 ws 访问/使用不受影响 |
| 新建入口位置 | `/settings` 表单 + ChatContainer 下拉（两者） | 统一管理中心 + chat 内就地新建 |
| 实现方案 | **A**：API 不变 + 后端校验前缀 + 前端预填引导 | 保护 SDK 消费者；安全权威在后端；UX 友好 |

## 5. 设计

### 5.1 后端

#### 5.1.1 新增 `security.ValidateWorkspaceWorkDir`

**文件**: `internal/security/path.go`

```go
// ErrWorkDirOutsideSandbox 由 ValidateWorkspaceWorkDir 在 work_dir 越出 owner 沙箱前缀时返回。
var ErrWorkDirOutsideSandbox = errors.New("security: work dir outside owner workspace sandbox")

// ValidateWorkspaceWorkDir 校验 dir 恰好等于或位于 owner 的 workspace 沙箱前缀下：
//   $HOME/.hotplex/workspaces/<ownerUserID>
// 这是 workspace 专用的额外约束，不替代通用 ValidateWorkDir（黑名单/symlink 仍需独立调用）。
// dir 必须已是绝对路径（调用前先经 config.ExpandAndAbs）。
func ValidateWorkspaceWorkDir(dir, ownerUserID string) error {
    home, err := os.UserHomeDir()
    if err != nil {
        return fmt.Errorf("security: cannot resolve $HOME: %w", err)
    }
    base := filepath.Join(home, ".hotplex", "workspaces", ownerUserID)
    rel, err := filepath.Rel(base, dir)
    if err != nil {
        return ErrWorkDirOutsideSandbox
    }
    // rel == "." 表示 dir == base（合法）；任何以 ".." 开头或含越界段的路径被拒。
    if rel == "." {
        return nil
    }
    if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
        return ErrWorkDirOutsideSandbox
    }
    return nil
}
```

**注意**：`filepath.Rel` 对 `dir` 不存在的情况同样有效（纯字符串逻辑），无需目录预先存在。

#### 5.1.2 `workspace_handlers.go` 追加调用

- **Create**（`workspace_handlers.go:102`，现有 `ValidateWorkDir(abs)` 之后）：
  ```go
  if err := security.ValidateWorkspaceWorkDir(abs, uid); err != nil {
      writeAppError(w, http.StatusForbidden, "WORK_DIR_OUTSIDE_SANDBOX",
          "work_dir must be under $HOME/.hotplex/workspaces/<your-user-id>")
      return
  }
  ```
- **Update**（`workspace_handlers.go:192`，现有 `ValidateWorkDir(abs)` 之后）：同上。
  - Update 仅在 `req.WorkDir != ""` 分支内校验 → **天然 grandfather**：老 workspace 不改 `work_dir` 就不触发；改 `name` / `agent_config_overrides` / `worker_preference` 也不触发。

#### 5.1.3 Grandfather 语义

- 无数据迁移脚本，无启动时校验。
- 老 workspace（含 Default 的 `./workspace`）数据库记录原样保留；session 继承其 `work_dir` 不重新校验。
- 仅"新建 workspace"或"更新 workspace 的 `work_dir`"两个动作强制前缀。

#### 5.1.4 work_dir 变更层级（澄清与 SwitchWorkDir 的关系）

work_dir 有两个互不冲突的变更层级（PR review 曾误读为契约矛盾，此处显式区分）：

| 层级 | 入口 | 可变性 | 约束 |
|------|------|--------|------|
| workspace 级 | `PATCH /api/workspaces/{id}` 携带 `work_dir` | **可改** | ① 落 owner 沙箱（G2，按 `OwnerUserID` 校验，admin 代改时按 owner 而非操作者）；② workspace 无活跃会话（`CountActiveSessionsInWorkspace` 守卫，返回 `409 WORKSPACE_NOT_EMPTY`） |
| session 级 | `POST /api/sessions/{id}/switch-workdir`（`/cd`） | workspace-bound session **不可改**（`400 WORK_DIR_IMMUTABLE`） | worker 必须跟随其 workspace 的 work_dir；platform/messaging session（`WorkspaceID==""`）仍支持 |

- workspace 级 PATCH 改的是 workspace 实体的 work_dir，影响该 workspace 所有**未来** session 的派生 key；活跃会话守卫确保变更瞬间无 session 受 key 漂移影响，故 resume 语义不破。
- session 级 SwitchWorkDir 是单 session 临时切换，对绑定 workspace 的 session 拒绝，避免 worker 脱离其 workspace 归属。
- §1 所述"session 从 workspace 继承 work_dir 且不可变"指 **session 级**（session 不能自行脱离 workspace）；与本节 workspace 级可改不矛盾。

### 5.2 前端（webchat）

#### 5.2.1 API 签名不变 + 新增路径 helper

- `lib/api/workspaces.ts` 的 `createWorkspace(name, workDir, signal?)` 签名**不变**（保护契约）。
- 新增 helper（同文件或 `lib/utils/workspace-path.ts`）：
  ```ts
  // 将 name 规整为安全的目录段：仅保留 [a-zA-Z0-9-]，小写，其余替换为 '-'。
  export function sanitizeWorkspaceDir(name: string): string { ... }

  // 拼接 owner 沙箱内的 work_dir。subdir 为空时用规整后的 name。
  export function buildWorkspaceWorkDir(uid: string, subdir?: string): string {
    const seg = subdir && subdir.trim() ? sanitizeWorkspaceDir(subdir) : sanitizeWorkspaceDir(name/*由调用方保证*/);
    return `~/.hotplex/workspaces/${uid}/${seg}`;
  }
  ```
  > `~/` 前缀由后端 `config.ExpandAndAbs`（`config_loader.go:324`）展开为 `$HOME`，前后端约定一致。

#### 5.2.2 共享表单组件 `NewWorkspaceForm`

**新文件**: `app/components/chat/NewWorkspaceForm.tsx`

- Props: `uid: string`、`onCreated(ws: Workspace): void`、`onCancel?: () => void`
- 字段：
  - `name`（必填，placeholder `My Project`）
  - `subdir`（可选，placeholder 留空则用 name）
- **实时预览**：`~/.hotplex/workspaces/<uid>/<seg>` 跟随输入更新，让用户直观看到最终路径。
- 提交：`createWorkspace(name, buildWorkspaceWorkDir(uid, subdir))`，错误（含新 `WORK_DIR_OUTSIDE_SANDBOX`）展示在表单。
- 复用现有输入框样式（参考已删的 `admin/workspaces/new` 的 `inputClass` / `labelClass`）。

#### 5.2.3 入口① `/settings` GeneralTab

**文件**: `app/components/chat/settings-modal/general-tab.tsx`

- 在现有 workspace 编辑区上方加"New Workspace"区块，嵌入 `<NewWorkspaceForm uid={user.id} onCreated={...} />`。
- `onCreated`：reload workspace 列表 + 切换 active workspace 到新建的（更新 localStorage `hotplex_active_workspace_id`）。
- 需从 `/settings` 页面把 `user.id` 透传给 GeneralTab（`/settings` 已 `getMe()` 拿到 `workspace`，需补传 `user`）。

#### 5.2.4 入口② ChatContainer 下拉 + Modal

**文件**: `app/components/chat/ChatContainer.assistant-ui.tsx`、`app/components/chat/NewWorkspaceModal.tsx`（新）

- 恢复之前删除的"New Workspace"下拉项（分隔线 + `<Link>`/`<button>`），点击改为打开本地 `NewWorkspaceModal`（而非跳转 admin）。
- `NewWorkspaceModal` 包一层 `<NewWorkspaceForm>`（复用 `NewSessionModal` 的 modal 壳样式）。
- ChatContainer 已 import `getMe`（`:25`），需在初始化时拿到 `user.id` 传给 modal/form。

#### 5.2.5 Default 自动创建修正

**文件**: `app/components/chat/ChatContainer.assistant-ui.tsx`（`loadWorkspaces`，约 `:111`）

```ts
if (list.length === 0) {
  const me = await getMe();
  const defaultWS = await createWorkspace(
    'Default Workspace',
    `~/.hotplex/workspaces/${me.id}/default`,
  );
  list = [defaultWS];
}
```

替换原 `'./workspace'`。新用户首次登录的 Default Workspace 自动落在沙箱内。

### 5.3 数据流

```
NewWorkspaceForm
  → createWorkspace(name, buildWorkspaceWorkDir(uid, subdir))
  → POST /api/workspaces { name, work_dir: "~/.hotplex/workspaces/<uid>/<seg>" }
  → handler.Create
      → config.ExpandAndAbs        // ~/ → $HOME，相对→绝对
      → security.ValidateWorkDir   // 黑名单 + symlink（通用）
      → security.ValidateWorkspaceWorkDir(abs, uid)  // 沙箱前缀（新）
      → store.CreateWorkspace      // work_dir 唯一约束
  ← Workspace JSON
```

### 5.4 错误码

| HTTP | code | 触发 | 状态 |
|---|---|---|---|
| 400 | `BAD_REQUEST` | name / work_dir 空 | 现有 |
| 400 | `INVALID_WORK_DIR` | `ExpandAndAbs` 失败 | 现有 |
| 403 | `WORK_DIR_FORBIDDEN` | `ValidateWorkDir` 黑名单 | 现有 |
| **403** | **`WORK_DIR_OUTSIDE_SANDBOX`** | **`ValidateWorkspaceWorkDir` 前缀越界** | **新增** |
| 409 | `WORK_DIR_TAKEN` | work_dir 唯一约束冲突 | 现有 |

前端 `extractApiError` 已能透传后端 `code`/message，新错误码自动在表单展示。

### 5.5 测试

**后端单测**（`internal/security/path_test.go` 扩展）：
- 前缀内通过：`$HOME/.hotplex/workspaces/<uid>`、`.../<uid>/sub`、`.../<uid>/a/b`
- 前缀外拒绝：`$HOME/.hotplex/workspaces/<other-uid>/x`、`/tmp/x`、`$HOME/other`
- 越界拒绝：`.../<uid>/../<other-uid>`、`.../<uid>/../../etc`
- **owner 隔离**：A 的 uid 前缀对 B 的 work_dir 必须拒绝
- symlink 越界：dir 是符号链接指向沙箱外 → 拒绝（`ValidateWorkDir` 已 eval symlink，前缀校验在 abs 之后即可；额外加测确认）

**后端集成**（`internal/gateway/workspace_handlers_test.go` 扩展）：
- `TestWorkspace_Create_OutsideSandbox_Rejected`：`work_dir=/tmp/x` → 403 `WORK_DIR_OUTSIDE_SANDBOX`
- `TestWorkspace_Create_InsideSandbox_Accepted`：`work_dir=$HOME/.hotplex/workspaces/<uid>/proj` → 201
- `TestWorkspace_Update_WorkDir_OutsideSandbox_Rejected`：PATCH 改 `work_dir` 越界 → 403
- `TestWorkspace_Update_Name_Grandfathered`：PATCH 只改 `name`（`work_dir` 仍为老的 `./workspace` 绝对路径）→ 200，不触发前缀校验
- `TestWorkspace_OwnerIsolation`：A 用户不能用自己 uid 前缀下的路径创建归属 B 的 workspace（由 `uid` 取自认证上下文天然保证，加测固化）

**前端**：
- `tsc --noEmit` 通过
- 手动验证：两处入口新建、预览路径、错误展示、Default 自动创建落在沙箱（Playwright 可选）

## 6. 影响范围

- **webchat 用户**：重获新建 workspace 能力；新建的 workspace 自动落在沙箱。
- **SDK / WS 消费者**：API 契约不变；但若其创建 workspace 时 `work_dir` 不在沙箱内，将收到 403 `WORK_DIR_OUTSIDE_SANDBOX` —— **这是预期的行为约束变更**，需在 SDK 文档/changelog 注明。
- **现有部署**：老 workspace 不受影响；新创建受沙箱约束。
- **消息平台（飞书/Slack）**：不受影响（PlatformSession 跳过 workspace 层）。

## 7. 开放问题（实现时确认）

- `sanitizeWorkspaceDir` 的精确规则（小写与否、最大长度、连续 `-` 折叠）—— 实现时遵循现有 `NAME_RE = /^[a-zA-Z0-9-]+$/` 风格，保持 name 与 dir 段一致。
- ChatContainer 拿 `user.id` 的时机（是否已在初始化流程内 `getMe`，避免重复请求）—— 实现时复用已有 `getMe` 调用结果。
