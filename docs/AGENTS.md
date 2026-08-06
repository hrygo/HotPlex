# HotPlex Docs — AGENTS.md

## OVERVIEW
Chinese-first self-hosted documentation source (DITA-style taxonomy). Built by `cmd/build-docs` (goldmark + chroma) into `internal/docs/out` and embedded in the binary — served at `http://localhost:8888/docs`. Change-driven maintenance is handled by the `hotplex-docs-patrol` skill (baseline at `.docs-patrol-baseline`).

## STRUCTURE
```
docs/
├── index.md / getting-started.md   # 文档中心入口
├── guides/        # contributor / developer / enterprise / user 分角色指南
├── tutorials/     # 场景教程（slack-integration、feishu-integration、cron-scheduled-tasks…）
├── reference/     # 参考手册：aep-protocol、events、configuration、cli、admin-api、metrics…
├── explanation/   # 概念解释
├── architecture/  # 架构设计（含 assets/architecture.svg）
├── security/      # 安全模型
├── specs/         # 设计规格（内部开发规格，76 篇）
├── v2/            # v2 路线图文档
├── superpowers/   # plans/ + specs/
├── archive/       # 归档（legacy-docs、specs）
└── swagger/       # swagger.json（swag 生成，勿手改）
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| 协议变更同步 | `reference/aep-protocol.md` + `reference/events.md` | AEP wire contract 变更时必须同步（根 AGENTS.md 强制规则） |
| 指标文档 | `reference/metrics.md` | 新计数器/直方图必须在此登记 |
| 用户入口文档 | `index.md` / `getting-started.md` | 面向新用户 |
| 规格评审 | `specs/` | 内部设计规格 |
| 文档构建 | `cmd/build-docs/main.go` | 输出到 `internal/docs/out`（go:embed） |

## CONVENTIONS
- **中文优先**：全部正文以中文撰写，必要时提供英文对照（webchat i18n 走 `webchat/locales/`，不在此处）
- **frontmatter**：每篇文档带 `title` / `weight` / `description`（docs-build 按 weight 排序）
- **`.editorconfig`**：`[*.md]` 保留行尾空白（`trim_trailing_whitespace = false`）——不要用工具清理
- **`internal/docs/out` 是构建产物**：只改 `docs/` 源文件，运行 `make docs-build` 重新生成；CI 校验产物一致
- **变更驱动巡逻**：版本发布或重大 PR 合并后按 `hotplex-docs-patrol` 流程审查文档影响

## ANTI-PATTERNS
- ❌ 手改 `internal/docs/out/` 或 `docs/swagger/swagger.json` — 生成产物，改源文件后重建
- ❌ 修改 AEP 事件/协议文档时不同步 `pkg/events`、Go SDK、三种示例 SDK 与双向协议测试
- ❌ 用翻译工具批量替换文档正文 — 术语需人工把关
- ❌ 手改搜索权重文件 — 由 `scripts/update_docs_weights.py` 生成，改完重新生成
- ❌ 直接删除 `docs/archive/` 内容 — 归档有保留目的，先确认引用
