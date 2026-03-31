# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`hotplex-worker` 是 HotPlex v1.0 的 Worker Gateway 统一接入层（Go 1.26）。
对外暴露统一的 WebSocket 全双工通信协议（AEP v1），屏蔽不同 AI Coding Agent 的协议差异。

> 更多背景见 `@docs/Worker-Gateway-Design.md`

## 架构约束（必须遵循）

- **黑盒集成**：Gateway 不侵入 agent 内部能力
- **控制面/数据面分离**：Gateway 仅持久化 session 元数据，不存储 agent 输出流
- **Tool 执行 = Autonomous**：Worker 自行执行 tool，`tool_call`/`tool_result` 仅通知 Client
- **竞态防护**：init 时加读锁、input 和 state 转换在同一互斥锁内完成、`SESSION_BUSY` 硬拒绝并发

## 目录结构

```
cmd/gateway/         # main.go 只做 wire 和信号拦截
internal/
  aep/               # AEP v1 协议定义
  config/            # 配置加载
  gateway/           # WS Gateway（握手/心跳/断线重连）
  session/           # Session Manager（状态机/SQLite WAL/GC）
  pool/              # Session 池化管理
  worker/            # Worker 核心接口 + 进程管理
    adapter.go       # SessionConn / Worker / Capabilities 接口
    proc.go          # 进程管理（分层终止）
    claude_code/
    opencode_cli/
    opencode_server/
    pimon/
  security/          # 认证
pkg/events/          # 跨模块可复用类型
api/                  # 协议 schema 文件
configs/              # 配置模板
scripts/              # 构建脚本
```

## Worker 接口契约

```go
// 所有 Worker Adapter 必须实现
var _ Worker = (*ClaudeCodeWorker)(nil)  // 编译时验证

type SessionConn interface {
    Send(ctx context.Context, msg *Message) error
    Recv() <-chan *Event
    Close() error
}
```

## 常用命令

```bash
gofmt -s -w .              # 格式化
golangci-lint run         # Lint
go build -pgo=auto ./cmd/gateway  # 构建（PGO）
go test -race ./...        # 测试（race 检测）
go mod tidy                # 清理依赖
```

## Session 状态

5 状态：`CREATED → RUNNING ↔ IDLE → TERMINATED → DELETED`
状态转换和 input 处理**必须在同一互斥锁内完成**。

> 详细状态机、TransitionWithInput、GC 策略见 `.claude/rules/session.md`

## Worker 适配差异（摘要）

| Worker | CLI / Transport | 关键差异 |
|---------|----------------|---------|
| Claude Code | `claude --print --session-id <id>` | `--resume` 恢复，session 在 `~/.claude/projects/` |
| OpenCode CLI | `opencode run --format json` | **无 `--session-id`**，从 `step_start` 提取 sessionID |
| OpenCode Server | `opencode serve`（HTTP+SSE） | `OpenCodeServerManager` 托管进程 |
| Pi-mono | stdio / raw stdout | ephemeral，无 session 恢复 |

## 并发规范

- mutex 显式命名 `mu`，零值即可，**禁止 embedding**
- goroutine 必须有 shutdown 路径（ctx cancel / close / WaitGroup）
- SQLite 写入通过**单写 goroutine** 串行化（WAL mode）

> 详细 mutex 规范、PoolManager 配额、GC shutdown 路径见 `.claude/rules/session.md`

## 编码风格（详细规则）

> 详细规则见 `.claude/rules/` 下的模块化文件

- `.claude/rules/golang.md` — Go 通用规范（合并 Uber Go Style + Go 1.26 特性）
- `.claude/rules/aep.md` — AEP v1 协议规范（编解码/消息路由/Backpressure/Seq 分配）
- `.claude/rules/security.md` — 安全规范（JWT/SSRF/Env 隔离/命令白名单/AllowedTools）
- `.claude/rules/session.md` — Session 规范（5 状态机/原子性/SESSION_BUSY/GC/mutex）
- `.claude/rules/testing.md` — 测试规范（test table、`testify/require`）
- `.claude/rules/worker-proc.md` — 进程管理规范（PGID 隔离/分层终止/output 限制）
- `.claude/rules/metrics.md` — 可观测性规范（Prometheus 命名/OTel Span/SLO）
