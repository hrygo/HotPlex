# HotPlex 版本面清单

本文件记录 HotPlex 当前已知的版本入口与豁免。它不能替代每次发布的动态扫描；新增 SDK、package、构建目标或 workflow 后，应先扫描，再补充本清单。

## 发布矩阵

每次发布建立矩阵，至少记录：path、field/context、old、target、action（update/regenerate/verify-only/exempt）、reason、evidence。

~~~bash
git grep -n -I -E 'v?[0-9]+[.][0-9]+[.][0-9]+' -- .
rg --files -g 'Makefile*' -g 'Dockerfile*' -g '*package*.json' -g 'pyproject.toml' -g 'pom.xml' -g '*.go' -g '.github/**' -g 'scripts/**' | sort
rg -n -I '(VERSION|version|ldflags|org[.]opencontainers[.]image[.]version|@version|Latest release|最新版本)' Makefile Dockerfile* .github scripts cmd docs examples webchat 2>/dev/null
~~~

git grep 覆盖 tracked 文件；rg 用于发现生成器和构建输入。不要用日志、node_modules、.next、构建输出或数据库内容判断发布是否干净。

## HotPlex 当前已知入口

路径可能迁移，字段可能改名；以下清单必须与动态扫描结果交叉核对：

| 类别 | 入口 | 处理方式 |
| --- | --- | --- |
| 核心构建 | Makefile 的 VERSION/LDFLAGS；cmd/hotplex/main.go 的 fallback version | update；验证 hotplex version |
| 容器 | Dockerfile 的 org.opencontainers.image.version | update；核对 tag/arg 注入 |
| API | cmd/hotplex/doc.go 的 @version；docs/swagger/swagger.json 的 info.version | 前者 update，后者 regenerate |
| 文档 | README.md、README_zh.md 的 badge 和 latest-release 文案 | update |
| 项目元数据 | AGENTS.md 顶部版本、日期、分支、基线提交 | update；hash 对应准备基线 |
| Java | examples/java-client/pom.xml 项目版本 | update；不要改 parent/dependency |
| Python | examples/python-client/pyproject.toml project version；hotplex_client/__init__.py 的 __version__ | update；两者对照 |
| TypeScript | examples/typescript-client/package.json；package-lock 根 metadata | update 或 verify-only，按实际字段处理 |
| WebChat | webchat/package.json；pnpm lockfile/importer | update 或 verify-only，按实际字段处理 |
| 发布链路 | .github/workflows/release.yml、scripts/、Docker/build actions | verify-only 或按命中 update |

清单之外发现新的分发入口时，必须加入矩阵；不能因为它不在本表就跳过。

## 常见豁免

- 历史 CHANGELOG.md、带日期的历史计划/设计/研究文档、旧版本迁移/兼容性说明。
- AEP aep/v1、协议/schema 版本、数据库 migration 编号、内置 package content hash/version。
- Go、Node、pnpm、swag、Spring Boot 等工具或第三方依赖版本。
- 明确用于防止硬编码产品版本的负向测试断言，例如 require.NotContains(..., "v1.41.0")；确认断言意图即可。

所有豁免都要写明路径、上下文和理由；无法分类时读取 troubleshooting.md，不要按命中数量猜测。

## 零遗留门禁

1. 重新扫描旧版本；除矩阵中的 exempt 外不得有旧版本命中。
2. 扫描目标版本，确认每个 update/regenerate 入口的结构化字段实际正确；README 命中不能证明 manifest 已更新。
3. 运行 Swagger/文档/WebChat 生成器和质量门，检查生成物 diff 只含预期变化。
4. 检查 manifest、lockfile、Docker/build metadata、workflow、最终二进制和 API metadata 的版本传递链。
5. 将矩阵摘要、豁免和未执行的外部动作写入报告；未解释残留时只能报告 blocked，不能说 ready/published。

命中数量不是验收标准：一处未解释的旧版本足以阻断，目标版本出现在错误文件也不能证明版本面完整。

## Release Notes 精确来源

Release body 必须直接使用 CHANGELOG.md 中目标版本的完整区块：从 `## [target]` 标题开始，到下一个 `## [` 标题之前结束。保留 Summary、Added、Changed、Fixed、Security 等原文、顺序和日期；不得使用 GitHub 自动生成的 What's Changed、commit 摘要、PR 列表或重新改写的文案。

推荐从发布 tag 的文件树提取，确保 Release 与被发布 commit 使用同一份内容：

~~~bash
tag=v1.42.0
notes_tmp=$(mktemp)
git show "$tag:CHANGELOG.md" | awk -v target="${tag#v}" '
  $0 == "## [" target "]" { in_section=1 }
  in_section && /^## \[/ && $0 != "## [" target "]" { exit }
  in_section { print }
' > "$notes_tmp"
test -s "$notes_tmp"
~~~

如果 release workflow 设置了 `generate_release_notes: true`，自动生成的 body 只是临时产物；在 Release 创建后必须执行 `gh release edit "$tag" --notes-file "$notes_tmp"`，再抓取 `gh release view "$tag" --json body --jq .body | sed '$d'` 与 `$notes_tmp` 做逐字 diff。`sed '$d'` 只去除 CLI 输出附带的末尾换行，不改变 Release body。只有比较结果为空，才能报告 Release Notes 已完成；否则报告 blocked，并保留差异。
