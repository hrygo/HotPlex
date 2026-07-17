# 发布流程常见陷阱和故障排除

## 目录

- [版本不一致](#版本不一致)
- [Tag 错误](#tag-错误)
- [Changelog 格式问题](#changelog-格式问题)
- [Release Notes 未更新](#release-notes-未更新)
- [CI 构建失败](#ci-构建失败)
- [发布后发现遗漏重要变更](#发布后发现遗漏重要变更)

---

## 版本不一致

**问题**：`cmd/hotplex/main.go`、`Makefile`、`CHANGELOG.md` 中的版本号不匹配。

**解决方法**：
1. 使用 SKILL.md 步骤 4 中的验证命令检查所有位置
2. 在提交前运行 `git diff` 确认所有版本相关文件都已更新
3. 在步骤 1 确定版本后，立即在一个地方记录目标版本，避免混淆

## Tag 错误

**问题**：创建了错误的 tag（版本号错误、拼写错误、在非 main 分支上创建）。

**解决方法**：
```bash
git tag -d vX.X.X                      # 删除本地 tag
git push origin :refs/tags/vX.X.X      # 删除远程 tag
git tag -a vY.Y.Y -m "Release vY.Y.Y"  # 重新创建
git push origin vY.Y.Y                 # 推送新 tag
```

**预防**：推送前用 `git tag -l` 和 `git tag -n9` 检查；只在 main 分支创建 tag。

## Changelog 格式问题

**问题**：缺少 Summary 部分、commit 消息直接复制过于技术化、格式不一致。

**解决方法**：
1. 始终从 SKILL.md 步骤 3 的模板开始
2. 撰写用户友好的描述：将 "Fix null pointer in session manager" 改为 "Gateway Core: Fix session crash when worker connection drops unexpectedly"
3. 即使版本很小也要写 Summary
4. 用 `head -50 CHANGELOG.md` 快速预览格式

## Release Notes 未更新

**问题**：GitHub Release 显示自动生成的 PR 摘要，而非完整 CHANGELOG 内容。

**原因**：CI 完成后忘记运行 `gh release edit --notes-file`；或 CHANGELOG.md 版本号与 tag 不匹配导致 awk 提取失败。

**解决方法**：
1. 手动编辑：`gh release edit vX.X.X --notes-file /tmp/release-notes.md`
2. 确认 CHANGELOG.md 版本号与 tag 完全一致（含 `v` 前缀）
3. 验证：`gh release view vX.X.X`

## CI 构建失败

**问题**：tag 推送后 CI workflow 失败。

**常见原因**：代码质量检查失败（`make quality`）、构建失败、测试回归。

**解决方法**：
```bash
gh run view <RUN_ID> --log             # 查看 CI 日志
# 修复问题后在 main 上提交
git commit -m "fix: CI failure for vX.X.X"
# 删除旧 tag，重新打 tag
git tag -d vX.X.X
git push origin :refs/tags/vX.X.X
git tag -a vX.X.X -m "Release vX.X.X"
git push origin main && git push origin vX.X.X
```

**预防**：步骤 5 完整运行 `make quality` + `make build` + `make check`；推送 tag 前确保工作目录干净。

## 发布后发现遗漏重要变更

**问题**：release 发布后发现遗漏了重要的 commit 或 changelog 条目。

**解决方法**：
1. 评估影响：用户可见的功能或重要修复需要补发版本
2. 补发 patch 版本（如 `v1.2.0` → `v1.2.1`），在 CHANGELOG.md 添加新版本说明，重新执行完整发布流程
3. 可选：在旧 release notes 中添加注释说明遗漏内容

**预防**：步骤 2 用 `git log --format="%h %s%n%b---"` 仔细审查所有 commit；步骤 3 按 scope 分组合并相关 commit。
