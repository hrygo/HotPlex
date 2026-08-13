# 可配置 Workspace 根目录：默认配置路径跟随 HOTPLEX_HOME

**日期**: 2026-08-12 · **分支**: 待建（`feat/configurable-workspace`） · **涉及版本**: v1.40.0+

---

## 1. 背景与目标

### 背景
用户反馈：workspace 默认固定在 `~/.hotplex` 目录，希望支持可配置；配置缺失时回退到 `~/.hotplex`。

### 现状（调研结论）
- `config.HotplexHome()`（`internal/config/config.go:165`）已支持 `HOTPLEX_HOME` 环境变量覆盖，缺失时回退 `~/.hotplex`（再回退 `TempBaseDir()`）。数据、日志、PID、scripts、agent-configs、skills 等所有状态目录均通过它派生，**已跟随** `HOTPLEX_HOME`。
- **真实缺口**：`DefaultConfigPath`（`config.go:160`）是硬编码常量 `"~/.hotplex/config.yaml"`。`config_loader.go:173` 的 `ExpandAndAbs` 展开 `~` 时使用 `os.UserHomeDir()`（真实用户主目录），**不尊重 `HOTPLEX_HOME`**。
- 后果：设置 `HOTPLEX_HOME=/data/hotplex` 后，数据/日志/PID 全部迁移到新目录，但默认配置文件仍从 `~/.hotplex/config.yaml` 加载——配置与数据分离，行为不一致。

### 目标（方案 A，用户已确认）
`DefaultConfigPath` 改为基于 `HotplexHome()` 解析：`HOTPLEX_HOME` 设置时返回 `$HOTPLEX_HOME/config.yaml`，未设置时返回 `~/.hotplex/config.yaml`。**一个环境变量控制整个 workspace（含配置文件）**，缺失时完整回退旧行为。

### 非目标
- 不做"配置文件内 `home:` 字段"（方案 B，鸡生蛋问题 + 需重构全局 `HotplexHome()` 约 20+ 调用点，已评估否决）。
- **显式 `--config` 始终优先**：通过 `cmd.Flags().Changed("config")` 判定"未指定"，显式传入的路径（**含恰好等于当前默认绝对路径**）一律不被 PID 状态替换。

---

## 2. 变更点清单

### 2.1 代码变更（8 个文件）

| # | 文件 | 位置 | 改动 |
|---|------|------|------|
| C1 | `internal/config/config.go` | `:160` | `const DefaultConfigPath` → `func DefaultConfigPath() string`，返回 `filepath.Join(HotplexHome(), "config.yaml")`；更新注释说明跟随 `HOTPLEX_HOME` |
| C2 | `cmd/hotplex/gateway_run.go` | `:214,:217` | 删除包级 `const defaultConfigPath`；`configFlag()` 内直接 `config.DefaultConfigPath()`（**必须实时调用**，避免包级 var 缓存导致测试/环境变化不生效） |
| C3 | `cmd/hotplex/admin_cmd.go` | `:26` | flag 默认值 → `config.DefaultConfigPath()`；帮助文案改为 `"配置文件路径（默认 $HOTPLEX_HOME/config.yaml，未设置时为 ~/.hotplex/config.yaml）"` |
| C4 | `cmd/hotplex/audit_cmd.go` | `:144,:230` | 同 C3（两处） |
| C5 | `cmd/hotplex/runtime_cmd.go` | `:78` | **语义升级**：`newFenceAdminClient` 的"未指定"判定从字符串相等改为**仅空字符串**（`configPath == ""`）——调用方（fence 子命令）在获取 flag 后执行 `if !cmd.Flags().Changed("config") { configPath = "" }`，确保显式传入"恰好等于默认绝对路径"也不触发 PID 替换 |
| C6 | `internal/cli/cron/client.go` | `:284` | 同 C5：`TriggerViaAdmin` 仅以空字符串判定"未指定"，`Changed("config")` 判断下沉到 cron 子命令调用方 |
| C7 | `cmd/hotplex/runtime_cmd_test.go` | `:114` | 传参改为 `""`（未指定的规范表示，替代旧默认值字符串） |
| C8 | `cmd/hotplex/gateway_cmd.go` | `:133` | `configPath == defaultConfigPath` → `!cmd.Flags().Changed("config")`（restart 时仅"用户未指定"才用实例原路径，显式传默认绝对路径也尊重） |

> 编译驱动：改完 C1 后 `go build ./...` 会列出全部残留引用，逐一处理。已 grep 确认无 `client/`、`webchat/` 或外部包引用。

### 2.2 测试变更

| # | 文件 | 内容 |
|---|------|------|
| T1 | `internal/config/config_test.go` | **新增** `TestDefaultConfigPath`（table-driven，**串行**——与 `config_test.go` 现有 `TestNormalizePath` 惯例一致，避免环境变量副作用相互污染）：① `HOTPLEX_HOME=/x` → `/x/config.yaml`；② `HOTPLEX_HOME` 清空 + `HOME=tmpDir` → `tmpDir/.hotplex/config.yaml`；③ `HOME` 不可用（清空）→ `TempBaseDir()/config.yaml`（与 `HotplexHome` 行为一致） |
| T2 | `cmd/hotplex/runtime_cmd_test.go` | 现有 `TestNewFenceAdminClient_UsesRunningConfigPath` 随 C7 改传参 `""` 后即回归验证：未指定 + PID state + `HOTPLEX_HOME` 设置 → 使用 running 实例配置。**不改断言** |
| T3 | `cmd/hotplex/runtime_cmd_test.go` | **新增** `TestNewFenceAdminClient_DefaultFallbackUnsetHome`：`t.Setenv("HOME", t.TempDir())` + `t.Setenv("HOTPLEX_HOME", "")`（空串=未设置）+ 写 PID state → `newFenceAdminClient("")` 必须使用 running 实例配置（验证未设置 `HOTPLEX_HOME` 时的 PID 回退路径仍生效） |
| T4 | `cmd/hotplex/runtime_cmd_test.go` | **新增** `TestNewFenceAdminClient_ExplicitDefaultPathNotReplaced`：设 `HOME`/`HOTPLEX_HOME` + 写 PID state（running.yaml 不同配置）→ 显式传入 `config.DefaultConfigPath()`（即恰好等于当前默认绝对路径）→ **必须使用显式路径对应的配置**，不被 PID 替换。注：本测试验证内部函数"仅空串为未指定"的语义（`newFenceAdminClient` 直接接收字符串）；调用方的 `Changed("config")` → `""` 转换是 cobra 层一行代码，由实现即所见保证，命令级测试为可选 |

### 2.3 文档变更

| # | 文件 | 改动 |
|---|------|------|
| D1 | `docs/reference/cli.md` | 全部 `~/.hotplex/config.yaml` 默认值（约 30 处表格）→ `$HOTPLEX_HOME/config.yaml`；在文档开头（或首个表格前）加说明行："未设置 `HOTPLEX_HOME` 时默认 `~/.hotplex/config.yaml`" |
| D2 | `docs/reference/configuration.md` | 环境变量表新增 `HOTPLEX_HOME` 条目：控制整个 workspace 根目录（含默认配置文件、数据、日志、PID），缺失时回退 `~/.hotplex`；在 `sqlite.path`/`pid_dir` 等 `~/.hotplex/...` 默认值条目旁无需逐个改（语义"以 workspace 根为基准"已准确） |
| D3 | `AGENTS.md`（及 `CLAUDE.md` 符号链接只编辑 `AGENTS.md`） | 配置参考节更新 `HOTPLEX_HOME` 描述：补充"同时决定默认配置文件路径（`$HOTPLEX_HOME/config.yaml`）" |
| D4 | `cmd/hotplex/gateway_cmd.go` | `:48` Long 文案 `(default: ~/.hotplex/config.yaml)` → `(default: $HOTPLEX_HOME/config.yaml when set, else ~/.hotplex/config.yaml)` |
| D5 | `cmd/hotplex/cron_cmd.go` | `:23` Long 文案 `(default: ~/.hotplex/config.yaml)` → 同上 |
| D6 | `internal/cli/cron/AGENTS.md` | `:50` 更新旧语义描述（"unset or equals DefaultConfigPath" → "仅 unset，显式路径永不被替换"） |

> `CHANGELOG.md` 为历史记录，不改。`scripts/`、`docker/` 脚本已按 env 使用，不改。

---

## 3. 执行步骤

1. **C1**：`internal/config/config.go` — `DefaultConfigPath` 改函数。
2. **C2–C8**：按上表逐一修改，`go build ./...` 确认零残留引用。
3. **T1**：新增 `TestDefaultConfigPath`；**T2** 随 C7 自动回归；**T3** 新增 HOME 回退测试；**T4** 新增显式默认路径不被替换测试。
4. **D1–D5**：文档与帮助文案更新。
5. **质量门禁**（见 §4 验收 1–2）。
6. **手动 E2E 验收**（见 §4 验收 3–5）。
7. 提交（conventional commit，如 `feat(config): resolve default config path from HOTPLEX_HOME`）。

---

## 4. 验收方案

### 验收 1：单元测试
```bash
go test ./internal/config/ -count=1 -race -shuffle=on -run TestDefaultConfigPath -v
# 期望：3 个 case 全 PASS
```

### 验收 2：回归 + 质量门禁
```bash
make lint                                        # golangci-lint 全绿
make docs-build                                  # 静态文档构建通过（D1 修改了 cli.md 表格）
go test ./internal/config/ ./cmd/hotplex/ ./internal/cli/cron/ -count=1 -race -shuffle=on
# 期望：全绿；重点关注：
#   - TestNewFenceAdminClient_UsesRunningConfigPath（HOTPLEX_HOME + PID 回退）
#   - TestNewFenceAdminClient_ExplicitConfigPathWins（显式路径优先）
#   - TestNewFenceAdminClient_DefaultFallbackUnsetHome（T3：未设置 HOTPLEX_HOME 的 PID 回退）
#   - TestNewFenceAdminClient_ExplicitDefaultPathNotReplaced（T4：显式传默认绝对路径不被替换）
```
若跑全量 `go test ./...`（单模块 ≤5s 约束），同样要求全绿。

### 验收 3：新 workspace 端到端（手动，daemon 模式）
```bash
export HOTPLEX_HOME=/tmp/hx-e2e
mkdir -p $HOTPLEX_HOME
# 写入最小 config.yaml（gateway.addr 用测试端口避免冲突，admin 配 token）
cat > $HOTPLEX_HOME/config.yaml <<'EOF'
gateway:
  addr: "localhost:18888"
admin:
  addr: "localhost:19999"
  tokens: ["e2e-token"]
EOF
hotplex gateway start -d          # daemon 模式，避免前台阻塞
# 轮询等待就绪（最多 ~10s）
for i in $(seq 1 20); do curl -sf http://localhost:18888/health >/dev/null 2>&1 && break; sleep 0.5; done
```
**断言**：
- **主断言（PID state）**：`jq -e '.config == "/tmp/hx-e2e/config.yaml"' "$HOTPLEX_HOME/.pids/gateway.pid"`（`GatewayState` JSON tag 为 `config`，见 `internal/cli/pidutil/pidutil.go:14`；`hotplex status` 不输出配置路径，**不作为**验证手段）
- **辅助断言（JSON 日志）**：若日志文件存在（daemon 模式 stderr 重定向或 `log.file.enabled`），`grep -F '"config":"/tmp/hx-e2e/config.yaml"' "$HOTPLEX_HOME/logs/gateway.log"`——默认 `log.format: json`，slog 输出为 JSON 字段形式，**不匹配** `config: xxx` 文本（日志格式不作为硬断言）
- `$HOTPLEX_HOME/.pids/` 生成、`$HOTPLEX_HOME/data/` 生成（`hotplex.db` 文件存在）；`$HOTPLEX_HOME/logs/` 仅当 `log.file.enabled` 时存在（**不作硬断言**，默认关闭）
- `hotplex status` 显示运行中（PID 状态解析一致）
- `curl -H "Authorization: Bearer e2e-token" http://localhost:19999/admin/health` 返回 200
- 清理：`hotplex gateway stop`；`rm -rf /tmp/hx-e2e`

### 验收 4：缺失回退回归（手动，隔离真实用户目录）
```bash
unset HOTPLEX_HOME
export HOME=/tmp/hx-fallback-home        # 隔离：不触碰真实 ~/.hotplex
mkdir -p $HOME/.hotplex
cat > $HOME/.hotplex/config.yaml <<'EOF'
gateway:
  addr: "localhost:18889"
admin:
  addr: "localhost:19998"
  tokens: ["fallback-token"]
EOF
hotplex gateway start -d
# 轮询 /health 就绪（同验收 3）
```
**断言**：加载 `$HOME/.hotplex/config.yaml`（即默认回退 `~/.hotplex`），数据/日志/PID 落于 `$HOME/.hotplex`；`hotplex status` 正常。清理：`hotplex gateway stop`；`rm -rf /tmp/hx-fallback-home`。

### 验收 5：帮助输出一致性
```bash
HOTPLEX_HOME=/tmp/hx-e2e hotplex gateway start --help | grep -F "/tmp/hx-e2e/config.yaml"
# 期望：默认值显示为该绝对路径（-F 避免正则特殊字符误匹配）
unset HOTPLEX_HOME
hotplex gateway start --help | grep -F "$HOME/.hotplex/config.yaml"
# 期望：默认值显示为 $HOME/.hotplex/config.yaml 绝对路径
```
> 注：不依赖完整 Cobra 输出格式（版本间可能变化），仅断言绝对路径子串出现。

---

## 5. 已知行为变化与风险

| 变化/风险 | 影响 | 处置 |
|-----------|------|------|
| `--help` 默认值从字面量 `~/.hotplex/config.yaml` 变为绝对路径 | 用户可见，但更准确 | 接受；帮助文案（C3/C4/D4/D5）补充说明 |
| "未指定"判定从字符串相等改为 `Flags().Changed("config")`（C5/C6/C8） | 行为升级：显式传任何路径（含默认绝对路径）均不再被 PID 替换；`""` 成为"未指定"的规范表示 | 计划内实现，T4 覆盖；`ExplicitConfigPathWins`/`UsesRunningConfigPath`/`DefaultFallbackUnsetHome` 覆盖其余分支 |
| `newFenceAdminClient("")` 在无 PID state 时 `ExpandAndAbs("")` 解析为当前目录相对路径 | 现有行为（旧代码空串分支相同），非本计划引入 | 无 |
| const → func 为 Go API 破坏性变更 | 仅包内/同仓库引用，已 grep 确认无外部引用 | 无 |
| 包级 var 缓存默认路径（若误用 `var defaultConfigPath = config.DefaultConfigPath()`） | 测试中 `t.Setenv` 后注册 flag 会拿到旧值 | C2 明确**禁止包级缓存**，`configFlag()` 内实时调用 |
| T1 串行 table-driven（`t.Setenv` 影响进程级环境） | 与 `config_test.go` 现有 `TestNormalizePath` 惯例一致；并行测试中的 Setenv 依赖 Go 版本行为，串行零风险 | T1 不加 `t.Parallel()` |

## 6. 回滚

- 单提交实现：`git revert <commit>` 即可完整回滚（含测试与文档）。
- 若已发布：旧二进制不受影响（改动仅影响新进程的默认路径解析），无需数据迁移——新旧 workspace 目录互不干扰，`HOTPLEX_HOME` 未设置的部署行为零变化。

## 7. 验收完成标准（DoD）

- [ ] `go build ./...` 零错误，`DefaultConfigPath` 无残留常量引用（含 `gateway_cmd.go`）
- [ ] `TestDefaultConfigPath`（T1）、`TestNewFenceAdminClient_DefaultFallbackUnsetHome`（T3）、`TestNewFenceAdminClient_ExplicitDefaultPathNotReplaced`（T4）新增并全绿；`internal/config`、`cmd/hotplex`、`internal/cli/cron` 回归全绿
- [ ] `make lint`、`make docs-build` 全绿
- [ ] 手动验收 3/4/5 全部通过（含清理环境）
- [ ] 文档 D1–D6 已更新

## 8. 评审修订记录

| 轮次 | 评审方 | 发现 | 处置 |
|------|--------|------|------|
| 1 | Momus | P1-1：遗漏 `gateway_cmd.go:133` 引用；建议 `Flags().Changed("config")` 区分显式/未指定 | 补 C8；`Changed()` 改造判定为独立优化，暂记录于风险表 |
| 1 | Momus | P1-2：T1 应串行（与 `TestNormalizePath` 惯例一致）；补未设置 `HOTPLEX_HOME` 的 PID 回退测试 | T1 改串行；新增 T3 |
| 1 | Momus | P1-3：E2E 前台阻塞、logs 默认不生成、帮助断言不可靠；漏 `cron_cmd.go:23` 文案与 `make docs-build` | 验收 3 改 `-d` + 轮询 + 断言 `.pids`/`data`；验收 5 改 `grep -F` 绝对路径；补 D5；验收 2 加 `make docs-build` |
| 2 | Momus | P1-A：字符串相等语义实际会变（显式传绝对默认路径改后被误判为未指定），与 §1"显式路径优先"矛盾 | 采纳 `Flags().Changed("config")` 方案：C5/C6/C8 仅以空字符串判定未指定，调用方 `Changed()` 下沉；新增 T4；§1 非目标改写 |
| 2 | Momus | P1-B：默认 `log.format: json`，验收 3 的 `config: xxx` 文本断言不匹配实际输出 | 主断言改为 PID state 的 `config_path` 字段；日志断言改为 JSON 字段 `grep -F '"config":"..."'` 且不作硬断言 |
| 3 | Momus | P1：§3 步骤 3 漏 T4；验收 2 引用已改名前的旧测试名且未列 T4 | §3 补 T4；验收 2 修正为实际 T3 名称并列出 T4 |
| 3 | Momus | P1：PID JSON tag 实为 `config`（`pidutil.go:14`）非 `config_path`；`hotplex status` 不输出配置路径 | 主断言改为 `jq -e '.config == "..."' "$HOTPLEX_HOME/.pids/gateway.pid"`；移除 status 验证表述 |
