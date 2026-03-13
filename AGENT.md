# 🤖 HotPlex: AI Agent Engineering Protocol

**Project Status**: v0.26.2 | **Core Role**: High-performance AI Agent Control Plane (Cli-as-a-Service).
This document defines the operational boundaries and technical DNA for AI agents working on **hotplex**.

---

## 1. System Philosophy & DNA
- **Cli-as-a-Service**: Bridge high-power AI CLIs (Claude Code, OpenCode) into production-ready interactive services.
- **Persistence**: Eliminate spin-up overhead via long-lived, isolated process sessions.
- **Tech Stack**: Go 1.25 | WebSocket Gateway | Regex WAF | PGID Isolation.

---

## Quick Start

```bash
make build        # 构建 hotplexd 守护进程
make test         # 运行单元测试
make test-race    # 运行竞态检测测试
make run          # 构建并前台运行
make lint         # 运行 golangci-lint

# Docker 部署
make docker-build # 构建镜像
make docker-up    # 启动服务
make docker-logs  # 查看日志
make docker-down  # 停止服务
```

环境配置：复制 `.env.example` 到 `.env` 并填写凭证。

---

## 2. Engineering Standards (AI Directive)

### 2.1 Technical Constraints
| Category        | Mandatory Guideline                                                                             |
| :-------------- | :---------------------------------------------------------------------------------------------- |
| **Concurrency** | Use `sync.RWMutex` for `SessionPool`. `defer mu.Unlock()` immediately. Zero Deadlock tolerance. |
| **Isolation**   | Spawn with PGID (`Setpgid: true`). Terminate via `-PGID` (Kill process group, not just PID).    |
| **State**       | `internal/engine/pool.go` is the **State Owner**. No redundant mapping in other layers.         |
| **Errors**      | Never `panic()`. Return explicit errors. Wrap with `%w`. Use `log/slog` with context.           |

### 2.2 Go Style & Integrity
- **Uber Style**: Follow [Uber Go Style Guide](.agent/rules/uber-go-style-guide.md). Verified interface compliance is mandatory.
- **Linter Signal**: Linter errors (e.g., `unused`) signify **incomplete integration**. **Link it, don't delete it.**
- **Testing**: Features require unit tests. Mock heavy I/O (use echo/cat). `go test -race` must pass.

---

## 3. Security Boundary Protocol
1. **WAF Bypass Forbidden**: No `Stdin` input shall reach the engine without `internal/security/detector.go:CheckInput()`.
2. **Capability Governance**: Prefer native CLI tool restrictions (`AllowedTools`) over manual path interception.
3. **Sandbox Hygiene**: Ensure CLI is initialized in its specific `WorkDir`. Avoid `sh -c` unless sanitized.

---

## 4. Architectural Map (Navigation for Agents)

- **Entrypoints**: `hotplex.go` (Public SDK), `client.go` (Interface), `cmd/hotplexd/` (Daemon).
- **Orchestration**: `engine/runner.go` (I/O Multiplexer & Singleton).
- **Intelligence**: `brain/` (Native Brain - orchestration, routing, memory compression).
- **Adapters (ACL Layer)**: 
    - `provider/`: Translates CLI protocols (Claude/OpenCode).
    - `chatapps/`: Translates social platforms (Slack/TG/Ding). `engine_handler.go` is the bridge.
- **Internal Core (Stability)**:
    - `internal/engine/`: `pool.go` (Pool/GC), `session.go` (Piping/PGID).
    - `internal/server/`: WebSocket & HTTP Gateway implementations.
    - `internal/security/`: `detector.go` (Regex WAF).
    - `internal/persistence/`: `marker.go` (Session durability).
    - `internal/secrets/`: Secrets provider (API key management).
    - `internal/telemetry/`: OpenTelemetry integration.
- **Systems**: `internal/sys/` (OS Signals), `internal/config/` (Watchers), `internal/strutil/` (High-perf utils).
- **Domain**: `types/` & `event/` (The "Universal Language" of the system).
- **Plugins**: `plugins/storage/` (Message persistence backends: SQLite, PostgreSQL).

---

## 5. Integrity & Multi-Agent Safety (CRITICAL)

### 5.1 Git Multi-Agent Protocol
In shared development, destructive commands destroy others' work.
- **STRICTLY FORBIDDEN**: `git checkout -- .`, `git reset --hard`, `git restore .`, `git clean -fd`.
- **MANDATORY CHECK**: Run `git status` before any git operation. 
- **SAFE ACTION**: Use `git checkout HEAD -- <specific-path>` or `git stash` for maintenance.
- **COMMIT FREQUENCY**: Commit/Push atomic, independent units of work often to "claim" progress.

### 5.2 Destructive Action Workflow
1. Run `git status` + `git diff --staged`.
2. Review all files; identify if changes belong to your current session.
3. If "dirty" files from other sessions exist, **request explicit user confirmation** before any broad git action.

---

## 6. AI File Editing Lifecycle (Zero-Corruption Rules)

The edit tool tracks file state. Sequential edits without re-reading cause duplicates/corruption.
1. **Read-Before-Edit**: Always `view_file` before editing, even if recently accessed. LINE#ID references must be fresh.
2. **One Edit Per Turn**: Maximum one logical block `edit`/`replace` call per response to prevent race conditions.
3. **Verify-After-Edit**: Immediately `view_file` the affected area. Confirm logic and formatting.
4. **Recovery**: If edit produces unexpected results, **STOP**, `view_file` fresh state, and `git checkout` if corrupted.

---

## 7. Action Execution Protocol
1. **Acknowledge**: State the technical plan briefly.
2. **Safety Check**: Check `git status` and architectural constraints in this document.
3. **Atomic Execution**: Write code in verifiable steps.
4. **Validation**: `go build ./...` and `go test` must pass before task completion.
5. **PR Creation**: **MANDATORY** to include `Resolves #<issue-id>` or `Refs #<issue-id>` in PR description body. This links the PR to the issue and enables automatic closure on merge.

## 8. Gotchas & Lessons Learned

### Configuration Pitfalls
- **Shell Default Syntax**: Go's `os.ExpandEnv` does NOT support shell-style defaults (`${VAR:-default}`). Use `${VAR}` only.
  - ❌ `${HOTPLEX_SLACK_BOT_USER_ID:-}` → Treated as literal variable name
  - ✅ `${HOTPLEX_SLACK_BOT_USER_ID}` → Works correctly

### Configuration Layering
- **Priority**: `.env` (host process) → `docker/matrix/.env-01` (Bot1 Docker) → `docker/matrix/.env-02` (Bot2 Docker) → `chatapps/configs/*.yaml` (behavior) → `docker/matrix/docker-compose.yml`
- **bot_user_id**: Each bot MUST have unique `bot_user_id` in .env, otherwise session IDs collide

---

**Mission Directive for AI Agents**: Extend HotPlex without compromising its structural density or safety. **Analyze twice, integrate once.**
