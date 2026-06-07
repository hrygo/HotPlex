---
name: hotplex-setup
description: HotPlex 生产环境安装、配置、部署与故障排查。以 `hotplex doctor` 诊断驱动，覆盖 onboard 向导、4 种 Worker 配置、STT/TTS、系统服务。当用户提到安装、配置、部署、doctor、onboard、环境检查、setup、启动失败、连接问题、凭证错误、服务无法启动时触发——即使用户只是描述了运行异常，也应先跑 doctor 诊断再排查。
---

# HotPlex 生产环境安装指引

以 `hotplex doctor` 为诊断核心。**先诊断再行动**——不要手动逐项检查依赖，doctor 集成了 25 个 checker（9 个 category），让它先跑。

整个流程幂等，重复运行只处理缺失项。

## 流程概览

```
安装二进制 → onboard → doctor → 按需修复 → service install → 验证
```

---

## Phase 1：安装

`hotplex` 已在 PATH 中 → 跳到 Phase 2。

### 1.1 快速安装脚本

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest --prefix ~/.local

# Windows (PowerShell)
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

### 1.2 源码构建（需 Go 1.26+）

```bash
git clone https://github.com/hrygo/hotplex.git && cd hotplex
make quickstart    # check-tools + build + test-short
```

### 1.3 首次配置向导

```bash
hotplex onboard              # 交互式（推荐首次）
hotplex onboard --non-interactive --enable-slack --slack-allow-from U0XXXXX  # CI/自动化
```

onboard 自动处理：平台凭据、Worker 选择、config.yaml/.env 生成、Agent 配置模板、STT/TTS 检查。

---

## Phase 2：诊断

```bash
hotplex doctor --json
```

参数：`--json`（机器可读）| `-v`（详细）| `-C <category>`（仅指定类别）| `--fix`（自动修复带 FixFunc 的 fail 项）

**报告结构**：

```json
{
  "summary": { "pass": N, "warn": N, "fail": N },
  "diagnostics": [{ "name": "category.check_name", "status": "pass|warn|fail", "message": "...", "fix_hint": "..." }]
}
```

**Exit codes**：0 = 全部通过 | 1 = 有 fail | 3 = 自动修复失败

**分支**：`fail: 0` → Phase 4 | `fail: N` → Phase 3 | 未安装 → Phase 1

---

## Phase 3：按分类修复

对每个 `status: "fail"` 项查下表。修复后 `hotplex doctor --json` 验证，循环直到 `fail == 0`。

### Checker 速查表

| Category | Checker | 失败原因 | 处理 |
|----------|---------|---------|------|
| environment | `go_version` | Go < 1.26 | 仅源码构建需要 |
| | `os_arch` | 不支持 | linux/macOS/windows + amd64/arm64 |
| | `build_tools` | lint 工具缺失 | 仅开发需要 |
| config | `exists` | config.yaml 不存在 | `hotplex onboard` |
| | `syntax` | YAML 解析错误 | 检查缩进，参考 `configs/config.yaml` |
| | `required` | API Key 缺失 | `hotplex onboard` 或手动设置 |
| | `values` | 端口/目录无效 | 创建目录或改端口 |
| | `env_vars` | ADMIN_TOKEN 缺失 | `.env` 中添加 |
| dependencies | `worker_binary` | CLI 不在 PATH | 安装 CLI 或设 `worker.<type>.command` / `HOTPLEX_WORKER_<TYPE>_COMMAND` |
| | `sqlite_path` | 数据目录不可写 | `mkdir -p ~/.hotplex/data && chmod 755 ~/.hotplex` |
| security | `admin_token` | Token 弱/空 | 替换为强随机值 |
| | `file_permissions` | 配置文件权限过宽 | `chmod 600 ~/.hotplex/.env ~/.hotplex/config.yaml` |
| | `env_in_git` | .env 被 git 追踪 | `git rm --cached .env` |
| runtime | `disk_space` | < 100MB | 清理磁盘 |
| | `port_available` | 8888/9999 被占 | 停止占用进程或改端口 |
| | `orphan_pids` | 残留 PID 文件 | `rm ~/.hotplex/.pids/*.pid` |
| | `data_dir_writable` | 数据目录不可写 | `chmod 755 ~/.hotplex/data` |
| messaging | `slack_creds` | Token 格式错 | Bot: `xoxb-`，App: `xapp-` |
| | `feishu_creds` | 凭据为空 | 检查飞书开放平台 |
| | `multi_bot_config` | bots[] 错误 | name 唯一、凭证非空、≤10 bot/平台 |
| stt | `stt.runtime` | funasr/ffmpeg 缺 | → `references/stt.md` |
| tts | `tts.runtime` | ffmpeg/MOSS 缺 | Edge TTS 仅需 ffmpeg。MOSS 需 python3 + 3GB。→ `references/tts.md` |
| agent_config | `suffix_deprecated` | 废弃文件名 | 重命名为子目录格式 |
| | `directory_structure` | 非标准文件 | 移除 |
| | `global_files` | 全局级影响所有 Bot | 考虑平台级/Bot 级 |

### 自动修复

```bash
hotplex doctor --fix    # 修复带 FixFunc 的 fail 项（config.exists、file_permissions、env_vars）
```

不修复 warn，不备份配置。修复前可 `cp ~/.hotplex/config.yaml{,.bak}`。

### 环境依赖（按需）

| 条件 | 依赖 | 参考 |
|------|------|------|
| TTS/STT 启用 | ffmpeg | `references/dependencies.md` |
| STT 本地 | ffmpeg + Python 3.10+ | `references/stt.md` |
| MOSS-TTS | ffmpeg + Python + 3GB 模型 | `references/tts.md` |

### 在线验证 Token

```bash
# Slack
curl -s -H "Authorization: Bearer <token>" "https://slack.com/api/auth.test"
# 飞书
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" -d '{"app_id":"<id>","app_secret":"<secret>"}'
```

---

## Phase 4：部署与验证

### 部署服务

```bash
hotplex service install          # 用户级（推荐，无需 root）
hotplex service start

sudo hotplex service install --level system  # 系统级
```

平台：Linux → systemd | macOS → launchd | Windows → SCM

### 验证清单

```bash
hotplex version                          # 版本号
hotplex doctor                           # 全通过
curl http://localhost:9999/admin/health  # Gateway（:9999）
curl http://localhost:8888/              # WebChat（:8888）
hotplex service logs -f                  # 平台连接正常
```

向用户展示配置摘要：

| 项目 | 值 |
|------|---|
| 版本 | vX.Y.Z |
| 平台 | Slack (N bots) / 飞书 (N bots) |
| Worker | claude_code / opencode_server / codex_cli / acp |
| STT/TTS | 配置状态 |
| 服务 | systemd / launchd / SCM |

---

## 配置陷阱

doctor 报告正常但行为异常时，排查这些高频问题：

### Worker Type 5 级 fallback

`WorkerType` 有 5 个来源（高→低）：

```
bots[].worker_type → <platform>.worker_type → HOTPLEX_MESSAGING_<PLATFORM>_WORKER_TYPE → messaging.worker_type → "claude_code"
```

4 种 worker：`claude_code`（stdio）/ `opencode_server`（HTTP+SSE）/ `codex_cli`（exec）/ `acp`（JSON-RPC 2.0）

**信号**：Worker type 不符预期 → 检查 env 变量是否覆盖了 YAML。

### `.env` 路径

| 路径 | 加载时机 |
|:---|:---|
| `~/.hotplex/.env` | `hotplex service`（**生产**） |
| `<project>/.env` | 开发模式 |

**信号**：改了 .env 无效 → 确认改的是 `~/.hotplex/.env`。

### `inject_exclude` 边界

可排除：`SOUL.md` / `AGENTS.md` / `SKILLS.md` / `USER.md` / `MEMORY.md`

`META-COGNITION.md` 是 `go:embed` 强制注入，**无法排除**。

### Worker 切换检查项

1. `worker.<type>.command` 正确（YAML 或 `HOTPLEX_WORKER_<TYPE>_COMMAND`）
2. 二进制在 PATH
3. ACP 单 bot 模式用全局 `worker.acp.command`（默认 `hermes acp`）

---

## Agent 个性化（可选后置步骤）

基础设施就绪后，如用户想定制 Agent：

1. 检查 `~/.hotplex/agent-configs/` 下文件是否仍为默认模板
2. 引导填写 USER.md（语言/角色/偏好）
3. 调整 SOUL.md 仅改用户明确要求的部分
4. 展示 diff 确认后写入

规则：幂等、最小变更、不覆盖已个性化内容。配置层级：全局 → 平台（slack/）→ Bot（slack/<botName>/）。

---

## 详细文档

| 文档 | 何时查阅 |
|------|---------|
| `references/dependencies.md` | Go/Python/ffmpeg 安装 |
| `references/stt.md` | STT 配置 |
| `references/tts.md` | TTS 配置 |
| `references/troubleshooting.md` | 端口/权限/服务问题 |
| `references/cross-platform.md` | 跨平台部署 |

## 版本升级

使用 `/hotplex-update` skill。升级后重新运行本 skill 验证。
