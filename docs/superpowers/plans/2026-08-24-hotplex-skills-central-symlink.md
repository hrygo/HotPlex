# HotPlex Skills 中心同步与逐项软链接 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 HotPlex 只把内置 Skill 内容同步到 `~/.agents/skills`，并在 Claude 等个性化 Agent 目录中按 Skill 创建安全、可审计的软链接。

**Architecture:** 保留现有中心目录事务和 Codex/OpenCode 共享 root；为 `Target` 增加可选 Agent alias root，Claude 的投影内容由中心目录提供，个性化目录只保存逐项软链接。receipt 增加 link 投影语义，旧 Claude 真实目录 receipt 在首次同步时仅在树哈希和所有权同时匹配时迁移。

**Tech Stack:** Go、`internal/skills/reconcile`、`os.Lstat`/`EvalSymlinks`/`Symlink`、JSON receipt、Testify。

**Spec:** `docs/superpowers/specs/2026-08-24-hotplex-skills-central-symlink-design.md`

## Global Constraints

- `~/.agents/skills` 是唯一实际内容同步目录；不得把整个 `~/.claude/skills` 替换成软链接。
- 已有正确软链接保持不动；真实目录、真实文件、错误链接和坏链不得自动覆盖。
- 不删除或移动用户非 HotPlex Skill；ACP 无文件系统投影。
- receipt、树哈希和回滚必须保持现有安全边界；测试不得写入真实 `$HOME`。
- 源码编辑使用 `apply_patch`，Go 文件最终使用 `gofmt`。

---

### Task 1: 扩展 reconcile 的中心 root 与 alias root 模型

**Files:**
- Modify: `internal/skills/reconcile/types.go`
- Modify: `internal/skills/reconcile/paths.go`
- Modify: `internal/skills/reconcile/reconciler.go`
- Modify: `internal/skills/reconcile/fs.go`
- Modify: `internal/skills/reconcile/reconcile_test.go`

**Interfaces:**
- `Paths.AliasRoots map[WorkerType]string` 保存 Agent 个性化 root；当前仅 Claude 使用 `~/.claude/skills`，中心 root 仍由 `NativeRoots[WorkerCodex]`/`NativeRoots[WorkerOpenCode]` 指向 `~/.agents/skills`。
- `Target.AliasRoots []string` 表示当前中心投影需要维护的个性化 root。
- `FileSystem.Symlink(oldname, newname string) error` 为生产文件系统和测试文件系统提供创建软链接能力。

- [ ] **Step 1: 写失败测试**

在 `internal/skills/reconcile/reconcile_test.go` 增加：

```go
func TestResolveTargetsUsesAgentsAsClaudeSourceAndClaudeAlias(t *testing.T) {
	userHome := t.TempDir()
	targets, err := ResolveTargets(userHome, []WorkerType{WorkerClaude})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, filepath.Join(userHome, ".agents", "skills"), targets[0].CanonicalRoot)
	require.Equal(t, []string{filepath.Join(userHome, ".claude", "skills")}, targets[0].AliasRoots)
}
```

- [ ] **Step 2: 运行 RED 测试**

Run: `go test ./internal/skills/reconcile -run TestResolveTargetsUsesAgentsAsClaudeSourceAndClaudeAlias -count=1`

Expected: FAIL because `Target` has no alias model and Claude currently resolves to `.claude/skills`.

- [ ] **Step 3: 实现最小模型**

将 Claude 的中心 `CanonicalRoot` 改为 `.agents/skills`，把 `.claude/skills` 放入 `AliasRoots`；合并 Claude/Codex/OpenCode 指向同一中心 root 时去重 alias。`normalizePaths` 为缺省 `AliasRoots[WorkerClaude]` 注入 `.claude/skills`，验证 alias root 必须位于 UserHome 下的 `.claude`，中心 root 必须位于 `.agents` 下。给 `osFS` 和 `recordingFS` 增加 `Symlink` 实现。

- [ ] **Step 4: 运行 GREEN 测试**

Run: `go test ./internal/skills/reconcile -run 'TestResolveTargets|TestNativeRootSymlink|TestInventorySymlink' -count=1`

Expected: PASS，且现有 ACP/路径越界测试继续通过。

### Task 2: 为 receipt 增加 link 投影并先写 link 状态失败测试

**Files:**
- Modify: `internal/skills/reconcile/receipts.go`
- Modify: `internal/skills/reconcile/types.go`
- Modify: `internal/skills/reconcile/inspection.go`
- Modify: `internal/skills/reconcile/reconcile_test.go`
- Create: `internal/skills/reconcile/links.go`

**Interfaces:**
- `Receipt.ProjectionType string`：空值兼容旧 tree receipt，`link` 表示逐项软链接 receipt。
- `Receipt.LinkTarget string`：link receipt 记录中心包的绝对路径。
- `inspectLinkProjection(fs FileSystem, linkPath, sourcePath string, manifest builtin.PackageManifest) (Item, Receipt, error)` 返回链接状态和所有权判断。

- [ ] **Step 1: 写失败测试**

覆盖以下状态：正确链接 unchanged、缺失链接 changed、整根 `.claude/skills -> .agents/skills` root-linked、错误链接 collision、真实目录 collision、link receipt 可解析且旧 v1 receipt 仍可解析。

示例：

```go
func TestClaudeSkillLinkStatus(t *testing.T) {
	r, paths := newTestReconciler(t)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	source := filepath.Join(paths.NativeRoots[WorkerCodex], manifest.Name)
	linkRoot := filepath.Join(paths.UserHome, ".claude", "skills")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0o644))
	require.NoError(t, os.MkdirAll(linkRoot, 0o755))
	require.NoError(t, os.Symlink(source, filepath.Join(linkRoot, manifest.Name)))

	report, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NotContains(t, reportReasons(report.Items), ReasonCollision)
}
```

- [ ] **Step 2: 运行 RED 测试**

Run: `go test ./internal/skills/reconcile -run TestClaudeSkillLinkStatus -count=1`

Expected: FAIL because the current inspection treats a package symlink as collision.

- [ ] **Step 3: 实现 link receipt 与检查**

保持旧 `schema_version: 1` tree receipt 可读；新增 link receipt 字段并在 `validReceipt` 中校验绝对 `LinkTarget`、`ProjectionType=link` 和 Claude alias。使用 `Lstat` 判断链接本体，使用 `EvalSymlinks` 验证已存在目标，不能跟随未知 link；对整根 alias root 先判断是否解析到中心 root，并返回 `root_linked` 状态。

- [ ] **Step 4: 运行 GREEN 测试**

Run: `go test ./internal/skills/reconcile -run 'TestClaudeSkillLinkStatus|TestReceipt|TestPackageTargetIdentity' -count=1`

Expected: PASS，旧 receipt 测试不回归。

### Task 3: 实现中心同步、逐项 link 创建和旧 Claude 目录迁移

**Files:**
- Modify: `internal/skills/reconcile/reconciler.go`
- Modify: `internal/skills/reconcile/projection.go`
- Modify: `internal/skills/reconcile/transaction.go`
- Modify: `internal/skills/reconcile/inspection.go`
- Create: `internal/skills/reconcile/links.go`
- Modify: `internal/skills/reconcile/reconcile_test.go`
- Modify: `internal/skills/reconcile/rollback_test.go`

**Interfaces:**
- `syncLinkedProjection(target Target, manifest builtin.PackageManifest) Item`：先确保中心包，再原子创建/迁移 alias link。
- `removeLinkedProjection(target Target, manifest builtin.PackageManifest) Item`：只删除有匹配 link receipt 的 link；中心包由中心 Worker receipt 单独管理。
- `ensureAliasLink(aliasRoot, packageName, sourcePath string) (Item, error)`：缺失时在父目录创建临时 link 后 rename，正确 link no-op，冲突 fail closed。

- [ ] **Step 1: 写失败测试**

覆盖：中心已有包时 Claude sync 只创建 symlink；缺失包创建 symlink；正确 symlink 二次 sync unchanged；真实目录/错误 symlink 不覆盖；整根 root symlink 不创建逐项 link；旧 Claude receipt 与真实目录树哈希匹配时迁移为 link；dry-run 不写任何路径。

- [ ] **Step 2: 运行 RED 测试**

Run: `go test ./internal/skills/reconcile -run 'TestSync.*Link|Test.*Symlink|Test.*DryRun' -count=1`

Expected: FAIL，因为当前同步会复制 Claude 目录且没有 link 写入路径。

- [ ] **Step 3: 实现最小事务**

在 `Sync` 的每个 target 中，中心 root 继续使用现有 `syncProjection`；带 `AliasRoots` 的 target 调用 `syncLinkedProjection`。link 创建流程为 `MkdirAll(parent)` → `MkdirTemp(parent, ".hotplex-link-*")` → 删除临时目录并调用 `Symlink(source, tempPath)` → `Rename(tempPath, linkPath)` → `SyncDir(parent)`。任何已存在的非预期项都直接返回 collision，不调用 `RemoveAll`。

旧 Claude 真实目录仅在旧 receipt 完整、`ProjectedTreeSHA256` 与中心树哈希一致、manifest/profile/package 全匹配时迁移；先移动到 backup，再创建 link receipt，成功后删除 backup 和旧 receipt。失败保留 backup 并返回 drift/failed。

- [ ] **Step 4: 实现 remove**

link remove 只接受 link receipt、link 目标精确指向中心包且 link 未被改写；先移到 tombstone，再删除 receipt，最后同步目录。若整个 alias root 是中心 root 的软链接，status 为 root-linked，remove 不删除它。

- [ ] **Step 5: 运行 GREEN 与回滚测试**

Run: `go test ./internal/skills/reconcile -run 'Test.*(Link|Symlink|Rollback|Remove|DryRun)' -count=1`

Expected: PASS，旧 tree projection 的事务/回滚测试继续通过。

### Task 4: 接入 CLI runner、更新文档和报告语义

**Files:**
- Modify: `cmd/hotplex/skills_cmd.go`
- Modify: `cmd/hotplex/skills_cmd_test.go`
- Modify: `docs/tutorials/skills-setup.md`
- Modify: `docs/reference/cli.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/tutorials/agent-personality.md`

**Interfaces:**
- `newSkillsRunner` 使用中心 `NativeRoots` 与 Claude `AliasRoots`。
- `status/sync/remove/dry-run` 保持原 flags，但报告 `linked`、`missing`、`collision`、`root_linked`。

- [ ] **Step 1: 写 CLI 失败测试**

更新 runner path 断言：Claude 的中心 root 为 `.agents/skills`，alias root 为 `.claude/skills`；Codex/OpenCode 仍共享 `.agents/skills`；ACP 仍不创建 root。

- [ ] **Step 2: 运行 RED 测试**

Run: `go test ./cmd/hotplex -run 'Test.*Skills|Test.*Update.*Skills' -count=1`

Expected: FAIL，当前 runner 仍把 Claude NativeRoot 指向 `.claude/skills`。

- [ ] **Step 3: 实现 runner 和报告文本**

仅修改 `newSkillsRunner` 的路径注入和现有报告字段映射；不新增 `.codex/skills` 或 `.config/opencode/skills` 的隐式映射。

- [ ] **Step 4: 更新文档**

把原先“Claude 独立复制”改为“中心 `.agents/skills` + Claude 逐项软链接”，明确 `.codex/skills` 不属于当前 HotPlex Worker root，ACP 无文件系统投影。

- [ ] **Step 5: 运行 CLI GREEN 测试**

Run: `go test ./cmd/hotplex -run 'Test.*Skills|Test.*Update.*Skills' -count=1`

Expected: PASS。

### Task 5: 迁移当前主机并完成验证

**Files:**
- No repository source files beyond prior tasks.
- Runtime targets: `~/.agents/skills`, `~/.claude/skills`, `~/.hotplex/state/skills`.

- [ ] **Step 1: 只读复核迁移对象**

确认 `~/.claude/skills/hotplex-cli` 与 `hotplex-operator` 均有匹配旧 receipt，中心目录对应包存在，树哈希一致；确认不处理 `.codex/skills` 和其他用户 Skill。

- [ ] **Step 2: 使用新源码执行 dry-run**

Run: `go run ./cmd/hotplex skills sync --profile runtime --worker claude_code --dry-run --json`；`go run ./cmd/hotplex skills sync --profile operator --worker claude_code --dry-run --json`。

Expected: 只报告两个 Claude link 将迁移/创建，不修改运行目录。

- [ ] **Step 3: 执行迁移**

Run: `go run ./cmd/hotplex skills sync --profile runtime --worker claude_code`；再对 `operator` 执行一次。

Expected: `~/.claude/skills/hotplex-cli`、`hotplex-operator` 变为指向 `~/.agents/skills` 的软链接，其他 Claude Skill 不变。

- [ ] **Step 4: 验证状态与幂等性**

Run: `go run ./cmd/hotplex skills status --profile runtime --worker claude_code --json`；`go run ./cmd/hotplex skills status --profile operator --worker claude_code --json`；重复运行 runtime 和 operator 两条 sync 命令。

Expected: status 报告 linked/unchanged；第二次 sync 不写入；`.codex/skills` 与非 HotPlex Claude Skill 时间戳/类型不变。

- [ ] **Step 5: 运行质量门禁**

Run: `gofmt -w`（仅变更 Go 文件）、`go test ./internal/skills/reconcile ./cmd/hotplex -count=1`、`go test ./... -count=1`、`go build ./...`、`git diff --check`。

Expected: 全部通过，且不提交或覆盖用户已有的无关工作树变更。
