---
name: hotplex-release
description: 通过判断 SemVer 影响、整理 CHANGELOG、对账版本面和验证发布输入来准备或发布 HotPlex 版本。不要用于主机二进制更新、运行时诊断或文档巡逻。
---

# HotPlex 发布

本 Skill 可以准备发布材料，但不会扩大写入权限。创建 tag、push commit/tag 或创建、
编辑 GitHub Release 前，必须有针对目标版本和 remote 的明确授权。仅要求版本分析、
changelog 或 release preparation 时，不得执行这些远端发布动作。发布只从 `main`
进行；非 `main` 分支只能准备并验证材料。

版本面清单与扫描规则见
[references/version-surfaces.md](references/version-surfaces.md)。每次发布都必须动态扫描当前仓库；清单是 HotPlex 已知入口的基线，不是可以替代扫描的固定文件列表。

Release body 的唯一事实来源是目标版本在 `CHANGELOG.md` 中的完整版本区块；不得用 GitHub
自动生成的 commit 摘要、PR 列表或重新改写的 release notes 代替它。

Release notes 有三个硬性不变量：提取结果必须非空、首行必须是目标版本标题
（兼容 `## [1.43.0] - 2026-08-25` 这类带日期标题）、远端 body 也必须非空。
本地 notes 文件或远端 body 为空时，禁止执行或完成 Release 更新；不能把两个空文件
比较相等当作校验通过。

## 路由

1. **确定范围**：读取当前分支、最新 release tag、tag 到 `HEAD` 的 commit 与 diff。
   根据用户可见影响选择 SemVer；breaking change 默认需要 major，除非项目已有明确的
   版本策略或用户指定了经确认的版本。
2. **发现版本面**：先读取版本面清单，再用 `rg --files`、`git grep` 和构建/发布配置搜索发现当前仓库的全部版本入口。至少覆盖核心构建、容器 metadata、文档/API metadata、所有 SDK/WebChat manifests、生成物来源、release workflow 和脚本；不要用命中数量代替逐项判断。建立版本矩阵，记录 `path`、字段/上下文、旧值、目标值、是否必须更新、豁免理由和验证方式。
3. **准备材料**：更新矩阵中确认属于当前产品版本的入口与 `CHANGELOG.md`。精确提取目标版本从 `## [target]` 开始、到下一版本标题之前的完整区块，保留原文、顺序、标题和日期；该区块就是 Release body，不能摘要、翻译、重排或拼接 commit 列表。条目按用户影响归并，并明确 breaking、迁移和安全影响；历史 changelog、带日期的历史文档、协议/依赖版本和明确的负向测试断言不得机械替换。
4. **验证**：先重新生成仓库要求的 Swagger、文档、WebChat 或其他发布生成物，再做版本一致性和零遗留检查：目标版本必须出现在矩阵中每个必须更新的入口；旧版本的每一个剩余命中必须落入已记录的豁免项；未解释的版本命中、manifest 与 lockfile 不一致、生成物漂移或版本注入不一致都阻断发布。保存并记录精确 changelog 区块后，必须验证 notes 文件非空且首行匹配目标版本，再核对 workflow 是否启用 `generate_release_notes`；若启用，发布后必须用该区块替换自动生成的 body。抓取远端 body 时去除 CLI 仅附带的一个末尾换行，先验证远端 body 非空，再与本地内容逐字比对；任一侧为空、body 不一致、目标版本区块缺失或版本标题不匹配都阻断完成声明。随后使用仓库当前质量门、构建流程和 release workflow 检查生成物与工作树。只把实际通过的检查表述为通过。
5. **发布**：只有当前请求明确授权该版本的 tag、push 和 GitHub Release，且已确认
   `main`、目标 commit、remote 与干净工作树时，才执行相应动作。若 workflow 自动生成
   notes，等待 Release 创建后立即用已通过非空/标题断言的精确 changelog 区块执行
   `gh release edit <tag> --notes-file <file>`，再完成“远端非空 + 逐字一致”校验。若
   提取失败、notes 文件为空、Release body 为空或比较双方均为空，立即停止并报告，不能
   继续发布收尾。发布后按当前 workflow 验证运行结果和实际产物，不假定固定 artifact
   数量。

遇到版本不一致、错误 tag、CI 失败、release notes 偏差或遗漏变更时，读取
[troubleshooting.md](references/troubleshooting.md)。任何删除 tag、改写已发布 Release、
强制 push 或补发版本都是新的外部状态变更；没有明确授权就停止并报告恢复选项。
