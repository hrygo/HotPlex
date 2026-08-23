---
name: hotplex-docs-patrol
description: Maintain HotPlex current documentation by mapping code, configuration, API, or release changes to BFS-reachable docs and repairing only verified drift. Do not use for ordinary copy editing, runtime diagnosis, or version publication.
---

# HotPlex docs patrol

文档巡逻请求授权在当前工作树内读取变更并修复确有影响的当前文档；它不自动授权
创建或切换分支、创建 Issue/PR、push 或其他远端写入。上述动作必须由当前请求明确授权。

## 巡逻

1. 读取 `.docs-patrol-baseline` 并验证其 commit；无有效基线时使用一个明确报告的、
   有界的近期窗口。审查该窗口内 commit 与 diff 的语义，而不只统计文件名。
2. 读取 [doc-registry.md](references/doc-registry.md)，把用户可见行为、配置、API、
   协议或运维变化映射到候选文档。Bug 修复和内部重构只有在当前文档因此失真时才
   需要修改。
3. 只维护 `docs/index.md` BFS 可达的当前文档。历史 archive、spec 和不可达记录不是
   本轮维护对象；先读取候选文档，再决定更新、补充、删除过时内容或保持不变。
4. 使用仓库当前文档构建命令验证导航、链接和生成结果。没有文档影响是有效结论，
   不为制造 diff 而编辑。

完成本地文档维护后，无论是否产生文档修改，都必须把当前 `HEAD` 写入本地忽略文件：

    git rev-parse HEAD > .docs-patrol-baseline

基线更新不是 commit、branch、Issue、PR 或 push 授权。若验证失败或巡逻尚未完成，
不要把不完整状态描述成已完成；报告已检查范围、修复、跳过项、验证结果和剩余风险。
