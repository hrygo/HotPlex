---
name: hotplex-release
description: HotPlex 版本发布工具 - 支持 patch/minor/major 版本发布，自动更新版本号、CHANGELOG，创建并推送 git tag，检查发布状态。当用户提到发布版本、创建 release、版本升级、打 tag、发布新版本时触发此 skill。
---

# HotPlex 版本发布 Skill

本 skill 提供完整的 HotPlex 版本发布流程，支持语义化版本控制（SemVer）的三种发布类型。

## 发布类型

| 类型 | 说明 | 示例 |
|------|------|------|
| **patch** | 补丁版本，修复 bug | 0.27.0 → 0.27.1 |
| **minor** | 次版本，新增功能 | 0.27.0 → 0.28.0 |
| **major** | 主版本，破坏性变更 | 0.27.0 → 1.0.0 |

## 工作流程

### 1. 确定发布参数

当用户发起发布时，必须明确以下信息：

1. **发布类型**：`patch` / `minor` / `major`
2. **发布说明**：新版本的更新内容（从 git log 或用户输入获取）
3. **发布原因**：用户指定的发布原因（功能、修复、优化等）

如果用户没有明确指定发布类型，询问用户并确认。

### 2. 文档防腐检查（仅限 Minor/Major 发布）

**触发条件**：发布类型为 `minor` 或 `major` 时必须执行

对于功能新增、较大功能优化、架构重构，必须检查并更新相关文档：

1. **检查新增/修改的源码包 README**：
   - 路径模式：`*/README.md`
   - 检查是否有新模块添加

2. **检查 API 文档**：
   - 路径模式：`docs/**/*.md`
   - 检查是否有 API 变更需要同步

3. **检查 Docker 文档**：
   - `docker/README.md`
   - `docker/matrix/README.md`

4. **检查配置文档**：
   - `docs/*.md`

如果发现文档未同步更新，提示用户并建议更新文档后再发布。

### 3. NotebookLM 同步（仅限 Minor/Major 发布）

**触发条件**：发布类型为 `minor` 或 `major` 时必须执行

对于功能新增、较大功能优化、架构重构，需要同步更新 NotebookLM：

1. **调用 NotebookLM Skill**：
   ```bash
   /hotplex-notebooklm
   ```

2. **同步内容**：
   - 新增/修改的源码包 README
   - API 文档变更
   - 架构设计文档
   - 配置说明

3. **执行方式**：使用 `Skill` 工具调用 `hotplex-notebooklm` skill

### 4. 版本号管理

**版本号位置**：`hotplex.go:13`
```go
Version      = "0.27.0"
```

根据发布类型计算新版本号：
- **patch**: `x.y.Z+1`
- **minor**: `x.y+1.0`
- **major**: `x+1.0.0`

### 5. CHANGELOG 更新

**CHANGELOG 位置**：`CHANGELOG.md`

使用以下模板在文件顶部插入新版本条目：

```markdown
## [v{x.y.z}] - {日期}

### Added
- {更新内容}

### Changed
- {变更内容}

### Fixed
- {修复内容}

---

## [v{旧版本}] - {旧日期}
...existing content...
```

日期格式：`YYYY-MM-DD`

### 6. Git 操作流程

1. **检查 git 状态**：确保工作区干净
2. **提交更改**：
   - 提交版本号更新
   - 提交 CHANGELOG 更新
3. **创建 tag**：`git tag -a v{x.y.z} -m "Release v{x.y.z}"`
4. **推送**：
   - 先推送代码：`git push origin main`
   - 再推送 tag：`git push origin v{x.y.z}`

### 7. 检查发布状态

Tag 推送后，检查以下内容：

1. **GitHub Release**：
   ```bash
   gh release view v{x.y.z}
   ```

2. **CI/CD 状态**：
   ```bash
   gh run list --branch main --status success
   ```

3. **下载链接验证**：
   - 检查是否生成了各平台的二进制文件
   - 检查 checksums.txt 是否正确

## 发布命令示例

### Patch 发布
```
发布 patch 版本：修复了 session 僵尸 marker 问题
```
执行：
1. 计算版本号：0.27.0 → 0.27.1
2. 更新 hotplex.go
3. 更新 CHANGELOG.md
4. 创建 tag v0.27.1
5. 推送并检查发布状态

### Minor 发布
```
发布 minor 版本：新增 AI 路由功能
```
执行：
1. 计算版本号：0.27.0 → 0.28.0
2. 更新 hotplex.go
3. 更新 CHANGELOG.md
4. 创建 tag v0.28.0
5. 推送并检查发布状态

## 重要约束

1. **永远不要直接修改 main 分支的版本号而不创建 tag**
2. **发布前确保所有测试通过**
3. **CHANGELOG 必须包含有意义的更新内容**
4. **推送 tag 后检查 GitHub Release 是否成功创建**
5. **如果发布失败，明确告知用户问题所在**
6. **Minor/Major 发布前必须进行文档防腐检查**
7. **文档更新必须与代码变更同步提交**

## 错误处理

| 错误场景 | 处理方式 |
|----------|----------|
| 工作区不干净 | 提示用户 stash 或 commit 当前更改 |
| GitHub Release 创建失败 | 检查 tag 是否已推送，尝试手动创建 |
| CI/CD 失败 | 告知用户失败的 workflow 和原因 |
| 版本号冲突 | 检查远程是否已有相同版本 tag |

## 输出格式

发布完成后，向用户报告：

```
✅ 版本发布完成！
- 版本号: v{x.y.z}
- Release: {release_url}
- 下载:
  - Linux: hotplex_{x.y.z}_linux_amd64.tar.gz
  - macOS: hotplex_{x.y.z}_darwin_arm64.tar.gz
  - Windows: hotplex_{x.y.z}_windows_amd64.zip
```
