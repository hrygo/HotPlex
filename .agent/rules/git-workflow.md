# Git 工作流 (Git Workflow)

## 快速流程 (Quick Start)

```
Issue → 分支 (Branch) → 开发 (Develop) → PR → 合并 (Merge)
```

## 关键命令 (Key Commands)

```bash
# 创建 Issue
gh issue create --title "[feat] 描述" --body "详细内容"

# 创建并切换到功能分支（引用 Issue #123）
# 命名规范: <type>/<issue-id>-<description>
git checkout -b feat/123-description

# 定期同步上游变更
git fetch origin && git rebase origin/main

# 提交变更 (需符合 Conventional Commits)
git commit -m "feat(scope): 描述信息 (Refs #123)"

# 创建 Pull Request
gh pr create --title "feat(scope): 描述信息" --body "Resolves #123"
```

## Commit 格式 (Commit Messages)

本项目严格执行 **Conventional Commits** 规范：

| 类型 (Type) | 说明 (Description) | 示例 (Example)                        |
| :---------- | :----------------- | :------------------------------------ |
| `feat`      | 新功能             | `feat(engine): 添加信号驱动状态机`    |
| `fix`       | Bug 修复           | `fix(pool): 修复并发创建时的竞态条件` |
| `refactor`  | 代码重构           | `refactor(session): 提取 IO 解析逻辑` |
| `docs`      | 文档更新           | `docs(readme): 更新安全架构图`        |
| `test`      | 测试用例           | `test(race): 增加并发压力测试`        |
| `chore`     | 杂项/依赖          | `chore(deps): 升级 Go 至 1.24`        |

## 分支命名规范 (Branch Naming)

- `feat/<issue-id>-描述`
- `fix/<issue-id>-描述`
- `refactor/<issue-id>-描述`

## Pull Request 规范 (PR Standards)

### 必须关联 Issue
PR 描述**必须**包含 Issue 链接，以确保可追溯性：

```markdown
Resolves #123    # 完成并自动关闭 Issue
Refs #123        # 仅关联，不自动关闭
```

### PR 描述模板
```markdown
## Summary
简要描述本次变更的核心逻辑。

Resolves #XXX

## Changes
- 变更点 1
- 变更点 2

## Test Plan
- [ ] 运行 `make test` 通过
- [ ] 运行 `go test -race ./...` 无并发冲突
- [ ] 环境手动验证
```

## 发布流程 (Release Workflow)

1. **合并 PR**：所有变更必须通过 PR 进入 `main` 分支。
2. **自动化检查**：确保所有 CI 状态检查（Tests/Linters）均为绿色。
3. **更新变更日志**：更新 `CHANGELOG.md`。
4. **发布版本**：
   ```bash
   git tag -a v0.X.X -m "Release v0.X.X"
   git push origin v0.X.X
   gh release create v0.X.X --notes "版本简要说明"
   ```

---
> [!IMPORTANT]
> **严禁直接向 `main` 分支推送代码。** 即使拥有绕过权限，也应通过 PR 流程进行开发，以确保项目交付质量。
