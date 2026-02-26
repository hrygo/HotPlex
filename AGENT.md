# 🤖 AI Agent Guidelines for hotplex

Welcome, AI Developer! This document serves as the top-level context and operational boundary for any AI Agent (like Claude Code, OpenCode, or Antigravity) working on the **hotplex** codebase.

Please read and strictly adhere to the following rules when analyzing, modifying, or creating code in this project.

---

## 1. Project Overview

**hotplex** is a high-performance **AI Agent Control Plane**.

- **First Principle**: Instead of reinventing the wheel, we leverage existing, powerful AI CLI agents (like Claude Code, OpenCode) and bridge them into production-ready systems by converting them into long-lived, interactive services (**Cli-as-a-Service**).
- **Core Role**: It provides a production-ready execution environment for AI agents, eliminating the continuous spin-up overhead of headless CLI mode and providing a unified control layer for security, state, and streaming.
- **Primary Language**: Go (Golang) 1.24
- **Architecture**: A lightweight Gateway (WebSocket) wrapping a Core Engine (`hotplex.Engine`), a session persistence layer (`internal/engine/pool.go`), and a strict Regex WAF (`internal/security/detector.go`).

### Repository

- **GitHub**: https://github.com/hrygo/hotplex
- **Owner**: `hrygo`
- **Repo**: `hotplex`
- **Version**: `0.11.5`

---

## 2. Coding Standards

### 2.1 SOLID & DRY Principles

When writing or refactoring Go code in HotPlex, you must enforce the following:

1. **Single Responsibility Principle (SRP)**:
   - Never dump mixed responsibilities into one struct or file.
   - Example: `runner.go` should only bridge I/O and OS processes. Session lifecycle belongs in `internal/engine/pool.go`. Danger detection belongs in `internal/security/detector.go`.

2. **Concurrency Safety First**:
   - **Never** read/write to `SessionPool` maps without holding the appropriate `sync.RWMutex`.
   - Always use `defer mu.Unlock()` immediately after acquiring a lock.
   - Be hyper-aware of Deadlocks. Do not trigger callbacks that might re-enter a locked section.

3. **Process Lifecycle & Zombie Prevention**:
   - Any OS Process created *must* be assigned a Process Group ID (PGID) via `SysProcAttr{Setpgid: true}`.
   - We kill processes by sending `SIGKILL` to `-PGID`, never just the PID, to ensure no orphan Node.js/Python processes leak.

4. **Error Handling**:
   - Do not use `panic()` in the core engine. Return `error` explicitly.
   - Wrap errors with context: `fmt.Errorf("failed to start session %s: %w", sessionID, err)`.

5. **Logging**:
   - Use `log/slog`.
   - Always include structured context: `logger.Info("session started", "session_id", sessionID)`.

### 2.2 Uber Go Style Guide

HotPlex follows the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md).

See **[.agent/rules/uber-go-style-guide.md](.agent/rules/uber-go-style-guide.md)** for the TOP 18 critical guidelines with examples.

| Category        | Key Rules                                                                                                          |
| --------------- | ------------------------------------------------------------------------------------------------------------------ |
| **Concurrency** | Zero-value mutexes • Defer cleanup • Channel size 0/1 • No fire-and-forget goroutines • Use `go.uber.org/atomic`   |
| **Errors**      | Never panic • Static errors via `var` • Wrap with `%w` • Handle errors once • Safe type assertions                 |
| **Quality**     | Verify interface compliance • No pointers to interfaces • Dependency injection • Use `time.Duration` • Consistency |

### 2.3 Linter & Code Integrity

Linter errors (like `unused`) are signals of **incomplete integration**, not junk to be deleted.

1. **No Lazy Deletions**: Never delete newly implemented logic or existing helpful code just to silence a linter error (e.g., `unused`). 
2. **Fix the Root Cause**: If a function is `unused`, it's an indicator that you forgot to link it to the main execution flow. Integrate it properly instead of removing it.
3. **Draft Preservation**: If code is for the future, use `//nolint:unused` with an explanation.

### 2.4 State & Lifecycle Ownership
1. **The Pool is Truth**: `internal/engine/pool.go` is the **exclusive owner** of process states. Do not maintain redundant maps of sessions in other layers.
2. **Stateless Runners**: `engine/runner.go` should remain a stateless dispatcher/orchestrator where possible.

---

## 3. Security Boundaries

HotPlex executes LLM-generated Shell commands on the host machine. **Security is the top priority.**

1. **Do Not Bypass `Detector`**: Never write code that allows user prompts or AI commands to reach `Stdin` without first passing through `CheckInput()` in `internal/security/detector.go`.
2. **Native Capability Governance**: Prioritize native tool restrictions (`AllowedTools` in `EngineOptions`) over file path interception. This leverages the CLI's internal sandbox for more reliable enforcement.
3. **Filesystem Isolation**: The agent's `WorkDir` is holy. Ensure the CLI is initialized with the correct working directory to leverage its native path restrictions.
4. **No Eval/Shell Hacks**: Do not use `sh -c` or `bash -c` unless strictly necessary and sanitized. Stick to direct binary execution via `os/exec` where possible.

---

## 4. Architecture Map

When looking for where to make changes, follow this map:

- **Public SDK (`/`)**:
  - `hotplex.go`: Main entry point with public aliases and engine initialization.
  - `client.go`: High-level client interface definitions for SDK users.
  - `cmd/hotplexd/`: Entry point for the HotPlex proxy server with `.env` and graceful shutdown logic.
- **Engine Layer (`engine/`)**:
  - `runner.go`: The `Engine` singleton. Handles session orchestration, event bridging, and I/O multiplexing.
- **Provider Layer (`provider/`)**: **[Anti-Corruption Layer - Provider]**
  - `provider.go`: Defines the `Provider` interface. Translates CLI-specific protocols into a normalized SDK format.
  - `factory.go`: Global factory for creating `claude`, `opencode`, and other CLI-based providers.
  - `event.go`: Unified event protocol (`ProviderEventType`) for standardized streaming.
- **ChatApps Layer (`chatapps/`)**: **[Anti-Corruption Layer - Platform]**
  - `engine_handler.go`: Translates social platform `ChatMessage` into Engine commands and bridges events back to platforms.
  - `manager.go`: Controls the lifecycle (start/stop) of all bot adapters.
  - `setup.go`: Unified entry point to boot multi-platform bots based on YAML configs.
  - **Adapters (Social)**: `telegram/`, `discord/`, `slack/`, etc. (Platform-specific handlers).
  - **Configs**: `configs/*.yaml`
- **Internal Core (`internal/engine/`)**:
  - `pool.go`: **State Owner**. Manages process hot-multiplexing, GC, and concurrency safety.
  - `session.go`: OS process piping, PGID management, and low-level I/O state machines.
- **Internal Persistence (`internal/persistence/`)**:
  - `marker.go`: Session durability markers to support seamless resumption across restarts.
- **Internal Security (`internal/security/`)**:
  - `detector.go`: The Regex WAF (System-wide input/output command interception).
- **Internal Configuration (`internal/config/`)**:
  - Configuration watchers and hot-reload logic.
- **Internal Systems (`internal/sys/`)**:
  - `proc_unix.go` / `proc_windows.go`: OS-level Process Group (PGID) isolation and signal routing.
- **Internal Utilities (`internal/strutil/`)**:
  - High-performance string manipulation and path cleaning.
- **Protocol Gateways (`internal/server/`)**:
  - `hotplex_ws.go`: Native JSON-over-WebSocket protocol (Public API gateway).
  - `opencode_http.go`: OpenCode HTTP/SSE compatibility layer (Translation gateway).
  - `security.go`: Shared security config for CORS and API Key authentication.
- **Types & Events (`types/`, `event/`)**: **[Universal Domain Types]**
  - Defines the "Internal Language" of the system, shared by all layers to avoid data leaks.
- **Integration Examples (`_examples/`)**:
  - Multi-language (Go, Node.js, Python) reference implementations for SDK usage, Bot integrations, and lifecycle management.

---

## 5. ChatApps Layer Integration

The `chatapps/` layer is the primary user-facing interface for many integrations. When modifying core engine logic, ensure that `chatapps/engine_handler.go` is updated if event structures or execution flows change. This layer enforces platform-specific constraints (e.g., Slack thread isolation) and should be treated as a first-class citizen alongside the WebSocket gateway.

---

## 6. Reference

### 5.1 OpenClaw (Local)

When needing to reference or learn from OpenClaw's implementation, the source code is available locally:

- **Path**: `/Users/huangzhonghui/openclaw`
- **Usage**: Read directly using the `Read` tool when implementing similar features or understanding patterns.
- **Note**: OpenClaw is the upstream project that HotPlex builds upon. Use it as a reference for:
  - Provider implementation patterns
  - Event parsing logic
  - CLI session management

---

## 6. Testing Requirements

- **No Code Without Tests**: If you add a feature, you must add a unit test in the corresponding `_test.go` file.
- **Mock Heavy I/O**: Do not write tests that actually spawn `npx @anthropic-ai/claude-code` unless it is explicitly an E2E test. Use echo/cat dummy shell scripts to mock the CLI in unit tests.
- **Race Detector**: All Go code must pass `go test -race ./...`.

---

## 7. Operational Safety

**This codebase belongs to the user. Data loss is non-negotiable.**

1. **Check Before Destructive Acts**: Before running `git checkout`, `git reset`, or `git clean`, you **MUST** run `git status`.
2. **Protect Unstaged Work**: 
   - If there are modifications not yet committed, **DO NOT** discard them without explicit confirmation.
   - Use `git stash` to safely move user work aside before performing maintenance.
3. **The "Checkpointed" Rule**: Encourage the user to `git add` or `git commit` frequently. Even if not committed, `git add` creates a "blob" that can be recovered with `git fsck --lost-found`.
4. **Micro-Commit Strategy**: Proactively suggest committing (or offer to do it) after completing a logically independent unit of work.
5. **Integrity over Silence**: NEVER delete functional code or complex logic just to pass a "pre-push" linter check. A linter warning is a prompt for **integration**, not a command for **destruction**.

---

## 7.1 CRITICAL: Multi-Agent Local Development Safety (HIGHEST PRIORITY)

**⚠️ STRICTLY FORBIDDEN: `git checkout -- .` without explicit user confirmation**

In a multi-agent local development environment (multiple AI agents working in the same directory simultaneously):

### Forbidden Commands (NEVER execute without explicit user confirmation):

| Command             | Risk                                                                       | Safe Alternative                       |
| ------------------- | -------------------------------------------------------------------------- | -------------------------------------- |
| `git checkout -- .` | ❌ **CRITICAL**: Discards ALL uncommitted changes (yours AND other agents') | `git checkout HEAD -- <specific-file>` |
| `git reset --hard`  | ❌ **CRITICAL**: Destroys ALL uncommitted work                              | `git stash` then `git stash pop`       |
| `git clean -fd`     | ❌ **HIGH**: Removes ALL untracked files                                    | Review files individually first        |
| `git restore .`     | ❌ **CRITICAL**: Same as `git checkout -- .`                                | `git restore <specific-file>`          |

### Why This Matters:

```
Agent A modifies: chatapps/processor_aggregator.go ✅ (not committed)
Agent B modifies: chatapps/processor_chain.go ✅ (not committed)
Agent A runs: git checkout -- . ❌ (destroys Agent B's work!)
```

### Mandatory Protocol:

1. **Before ANY destructive git command**:
   - Run `git status` and **review ALL changed files**
   - Check if changes belong to you (your session) or other agents
   - Ask for explicit user confirmation if other agents' work is present

2. **Use targeted commands**:
   ```bash
   # ✅ SAFE: Only restore YOUR specific file
   git checkout HEAD -- chatapps/processor_aggregator.go
   
   # ❌ FORBIDDEN: Restore everything (destroys others' work)
   git checkout -- .
   ```

3. **Use `git stash` instead**:
   ```bash
   # ✅ SAFE: Temporarily save all changes
   git stash push -m "WIP: agent session backup"
   
   # Later, restore everything
   git stash pop
   ```

4. **Commit frequently**:
   - After each logical unit of work, commit your changes
   - This protects your work and signals to other agents what's "claimed"

### Violation Consequence:

**Running `git checkout -- .` without confirmation in a multi-agent environment is the HIGHEST PRIORITY violation.** It can destroy hours of work from other agents and cannot be easily recovered.

---

## 8. Action Mode Trigger

If the USER asks you to `[Implement]`, `[Extend]`, or `[Fix]` something in HotPlex:

1. **Acknowledge**: Briefly state the plan.
2. **Safety Check**: Check `git status`. If the repo is "dirty", ask the user if you should `stash` or `commit` their changes first.
3. **Verify**: Check this `AGENT.md` for architectural constraints.
4. **Execute**: Write the code in **atomic steps**. 
5. **Auto-Clean**: After each major step, check if it's a good time to commit.
6. **Validate**: Ensure it builds (`go build ./...`) and tests pass.
7. **GitHub Operations**: Prioritize using the `gh` command for any GitHub-related actions (Releases, PRs, Runs).

---

## 9. File Editing Workflow (CRITICAL)

**The Edit tool tracks file state. Sequential edits without re-reading cause duplicates and corruption.**

### 9.1 Mandatory Rules

1. **One Edit Per Turn**: Never send multiple `edit` calls to the same file in a single response.
   ```
   ❌ edit(file) → edit(file) → edit(file)  # WRONG: Will cause duplicates
   ✅ edit(file) → read(file) → verify → next edit
   ```

2. **Read-Before-Edit**: Always read the file before editing, even if you "just read it".
   - File state changes between turns
   - LINE#ID references become stale immediately after any edit

3. **Verify-After-Edit**: After each edit, read the file to confirm the result before proceeding.
   ```
   ✅ read(file) → edit(file, target) → read(file) → confirm correct → continue
   ```

### 9.2 Common Failure Patterns

| Pattern                        | Result            | Fix                 |
| ------------------------------ | ----------------- | ------------------- |
| `edit` → `edit` same file      | Duplicate content | Read between edits  |
| Using stale LINE#ID            | Wrong position    | Re-read before edit |
| Assuming edit succeeded        | Silent corruption | Verify after edit   |
| Multiple edits in one response | Race condition    | One edit per turn   |

### 9.3 Batch Editing Strategy

When multiple changes are needed in one file:
1. **Prefer single large edit**: Combine all changes into one `edit` call with multi-line `lines`
2. **If must do sequential**: `read` → `edit` → `read` → `confirm` → `edit` → `read` → `confirm`
3. **If edit fails**: `read` the file again to get fresh state before retrying

### 9.4 Error Recovery

If an edit produces unexpected results:
1. **STOP** - Do not attempt another edit
2. **READ** - Get current file state
3. **ANALYZE** - Understand what went wrong
4. **RESTORE** - Use `git checkout` if needed to get clean state
5. **RETRY** - Start fresh with correct file state

---

## 10. Quick Reference Summary

| Category        | Requirement                                                                |
| :-------------- | :------------------------------------------------------------------------- |
| **Concurrency** | `sync.RWMutex` required • `defer mu.Unlock()` immediately • PGID isolation |
| **Safety**      | `git status` check mandatory • No lazy deletions • No WAF bypass           |
| **Code**        | SOLID • Uber Go Style • `slog` structured logging                          |
| **Testing**     | `go test -race` required • Mock I/O • No code without tests                |
| **Editing**     | Read-before-edit • Verify-after-edit • One edit per turn                   |
