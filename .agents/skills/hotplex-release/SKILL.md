---
name: hotplex-release
description: Prepare or publish a HotPlex version by selecting SemVer impact, curating CHANGELOG entries, reconciling version surfaces, validating release inputs, tagging, and creating the GitHub Release. Do not use for host binary updates, runtime diagnosis, or documentation patrol.
---

# HotPlex release

本 Skill 可以准备发布材料，但不会扩大写入权限。创建 tag、push commit/tag 或创建、
编辑 GitHub Release 前，必须有针对目标版本和 remote 的明确授权。仅要求版本分析、
changelog 或 release preparation 时，不得执行这些远端发布动作。发布只从 `main`
进行；非 `main` 分支只能准备并验证材料。

## 路由

1. **确定范围**：读取当前分支、最新 release tag、tag 到 `HEAD` 的 commit 与 diff。
   根据用户可见影响选择 SemVer；breaking change 默认需要 major，除非项目已有明确的
   版本策略或用户指定了经确认的版本。
2. **准备材料**：从当前源码、构建配置、SDK manifests、API metadata 和 release
   workflow 发现版本表面，不依赖固化位置或数量。更新相关版本与 `CHANGELOG.md`，
   条目按用户影响归并，并明确 breaking、迁移和安全影响。
3. **验证**：使用仓库当前质量门、构建流程和 release workflow 检查版本一致性、
   生成物和工作树。只把实际通过的检查表述为通过。
4. **发布**：只有当前请求明确授权该版本的 tag、push 和 GitHub Release，且已确认
   `main`、目标 commit、remote 与干净工作树时，才执行相应动作。发布后按当前
   workflow 验证运行结果和实际产物，不假定固定 artifact 数量。

遇到版本不一致、错误 tag、CI 失败、release notes 偏差或遗漏变更时，读取
[troubleshooting.md](references/troubleshooting.md)。任何删除 tag、改写已发布 Release、
强制 push 或补发版本都是新的外部状态变更；没有明确授权就停止并报告恢复选项。
