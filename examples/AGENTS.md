# SDK Examples — AGENTS.md

## OVERVIEW
Three reference client SDKs (TypeScript / Python / Java) sharing the AEP v1 wire contract. Each is an independent toolchain with its own README; all three are checked against the same 38 golden envelopes from `pkg/aep/schema/corpus` in CI.

## STRUCTURE
```
examples/
├── typescript-client/   # npm/pnpm: src/ + tests/（vitest）
├── python-client/       # pip: hotplex_client/ 包 + tests/（pytest）
└── java-client/         # Maven: src/main/java/dev/hotplex/protocol + conformance test
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| SDK 使用入口 | `examples/<sdk>/README.md` | 各语言用法、安装、示例 |
| 协议一致性测试 | `tests/conformance.test.ts` / `tests/test_conformance.py` / `.../conformance/AepCorpusConformanceTest.java` | 消费 38 个 golden envelopes |
| 协议编解码 | TS `src/envelope.ts` · Python `hotplex_client/` · Java `protocol/` | AEP envelope codec |
| 运行示例 | 各 SDK `examples/` 目录 | 独立可运行 demo |

## CONVENTIONS
- **同步契约**：修改 `pkg/events` 的 Kind/Data/JSON tag 时，三个 SDK 的事件类型与 conformance 测试**必须**同步更新（根 AGENTS.md AEP wire contract 规则）
- **corpus 不可手改**：golden 文件由 `pkg/aep/schema/generate_corpus.go` 确定性生成；需要新 fixture 时改 Go 侧后 `go run ./cmd/gen-corpus`
- **各自独立构建**：TS 用 `pnpm test`（vitest）、Python 用 `pytest`、Java 用 Maven —— 三个工具链互不依赖，CI 以并行 job 验证
- **target/、dist/、node_modules/ 不入库**：Java `target/` 与 TS `dist/` 均为构建产物

## ANTI-PATTERNS
- ❌ 只改 Go 侧事件类型就提交 — 三 SDK 类型/测试与 `docs/reference/events.md` 必须同行
- ❌ 手改 `pkg/aep/schema/corpus/*.json` 绕过生成器 — schema_test drift guard 会在 CI 拒绝
- ❌ 在 SDK 里引入与网关版本不匹配的事件字段 — 新字段必须向后兼容（omitempty 风格）
- ❌ 用单一 SDK 的测试替代三 SDK 一致性验证 — CI 并行跑全部四个客户端
