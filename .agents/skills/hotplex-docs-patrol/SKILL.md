---
name: hotplex-docs-patrol
description: Maintain HotPlex current documentation by mapping code, configuration, API, or release changes to BFS-reachable docs and repairing only verified drift. Do not use for ordinary copy editing, runtime diagnosis, or version publication.
---

# HotPlex docs patrol

文档巡逻请求授权在当前工作树内读取变更并修复确有影响的当前文档；它不自动授权
创建或切换分支、创建 Issue/PR、push 或其他远端写入。上述动作必须由当前请求明确授权。

## 巡逻

1. 读取 `.docs-patrol-baseline` 并验证其 commit；无效或缺失时使用一个明确报告的、
   有界的近期窗口。审查该窗口内 commit 与 diff 的语义，而不只统计文件名。推荐的
   只读基线流程是：

       BASELINE_FILE=.docs-patrol-baseline
       if [ -s "$BASELINE_FILE" ] && git rev-parse --verify "$(cat "$BASELINE_FILE")^{commit}" >/dev/null 2>&1; then
           BASELINE=$(cat "$BASELINE_FILE")
       else
           BASELINE=$(git log --since="7 days ago" --reverse --format="%H" | head -1)
           # 报告使用了 fallback；若窗口为空，停止并请求明确范围。
       fi

   使用 `git log --no-merges "$BASELINE..HEAD"` 和针对 `internal/`、`cmd/`、`pkg/`、
   `configs/`、`docs/` 的有界 diff，逐条理解变更意图。
2. 读取 [doc-registry.md](references/doc-registry.md)，把用户可见行为、配置、API、
   协议或运维变化映射到候选文档。Bug 修复和内部重构只有在当前文档因此失真时才
   需要修改。
3. 只维护 `docs/index.md` BFS 可达的当前文档。历史 archive、spec 和不可达记录不是
   本轮维护对象；先读取候选文档，再决定更新、补充、删除过时内容或保持不变。
4. 只修改 `docs/` 源文件，不直接编辑生成目录。按仓库当前 Makefile 验证导航、链接
   和生成结果；当前入口是 `make docs-build`，随后可运行 `make docs-lint`。如果命令、
   输出目录或生成器发生变化，以 `Makefile` 和 `docs/AGENTS.md` 为准。没有文档影响
   是有效结论，不为制造 diff 而编辑。

只有完成变更分析、候选文档判断和文档构建/链接验证后，才把当前 `HEAD` 写入本地
忽略文件：

    git rev-parse HEAD > .docs-patrol-baseline

基线更新不是 commit、branch、Issue、PR 或 push 授权。若验证失败或巡逻尚未完成，
保留旧 baseline，报告 blocked，不要把不完整状态描述成已完成。

## 报告收尾

报告至少记录：旧 baseline 或 fallback 窗口、检查的 commit 数、候选文档数、实际修改
的文档、明确跳过及其理由、`make docs-build`/`make docs-lint` 结果、baseline 是否更新，
以及未执行的外部动作。没有文档修改时也要说明“候选文档已阅读且当前仍准确”，而不是
只报告“无 diff”。
