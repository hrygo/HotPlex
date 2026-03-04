# HotPlex 配置文件管理方案 - 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现统一的配置目录搜索机制，支持 `~/.hotplex/` 作为用户配置根目录

**Architecture:**
- 修改 `chatapps/setup.go` 中的配置目录搜索逻辑
- 添加分层搜索：`~/.hotplex/configs/` → `./chatapps/configs/`
- 向后兼容现有 `CHATAPPS_CONFIG_DIR` 环境变量

**Tech Stack:** Go, godotenv, YAML

---

## Task 1: 修改配置目录搜索逻辑

**Files:**
- Modify: `chatapps/setup.go:25-32`

**Step 1: 查看当前代码**

```bash
# 查看当前 Setup 函数开头
cat -n chatapps/setup.go | head -40
```

**Step 2: 修改配置目录搜索逻辑**

在 `chatapps/setup.go` 中，修改 `Setup` 函数：

```go
// Setup initializes all enabled ChatApps and their dedicated Engines.
// It returns an http.Handler that handles all webhook routes.
func Setup(ctx context.Context, logger *slog.Logger) (http.Handler, *AdapterManager, error) {
	// 配置目录搜索优先级：
	// 1. CHATAPPS_CONFIG_DIR (向后兼容)
	// 2. ~/.hotplex/configs (用户配置)
	// 3. ./chatapps/configs (默认)
	configDir := os.Getenv("CHATAPPS_CONFIG_DIR")

	if configDir == "" {
		// 尝试用户配置目录
		homeDir, err := os.UserHomeDir()
		if err == nil {
			userConfigDir := filepath.Join(homeDir, ".hotplex", "configs")
			if _, err := os.Stat(userConfigDir); err == nil {
				configDir = userConfigDir
				logger.Debug("Using user config directory", "path", configDir)
			}
		}
	}

	if configDir == "" {
		configDir = "chatapps/configs"
	}
	// ... 后续代码保持不变
```

**Step 3: 添加 filepath import**

确保 import 包含 `path/filepath`：

```go
import (
	// ... existing imports
	"path/filepath"
)
```

**Step 4: 验证编译**

```bash
cd /Users/huangzhonghui/.slack/BOT_U0AHRCL1KCM/hotplex
go build ./...
```

Expected: 编译成功，无错误

**Step 5: Commit**

```bash
git add chatapps/setup.go
git commit -m "feat(chatapps): add hierarchical config directory lookup"
```

---

## Task 2: 添加用户配置目录自动创建功能（可选增强）

**Files:**
- Modify: `chatapps/setup.go`

**说明：** 此任务为可选，如果用户目录不存在则静默 fallback 到默认目录，不需要强制创建。

---

## Task 3: 更新环境变量文档

**Files:**
- Modify: `.env.example`
- Modify: `docs/docker-deployment_zh.md`

**Step 1: 更新 .env.example**

在 `.env.example` 末尾添加：

```bash
# ------------------------------------------------------------------------------
# 7. 📁 CONFIGURATION
# ------------------------------------------------------------------------------

# Configuration root directory
# Default: ~/.hotplex
# HOTPLEX_CONFIG_DIR=/path/to/config

# Environment file path (loaded by godotenv)
# Default: {HOTPLEX_CONFIG_DIR}/.env or .env in working directory
# ENV_FILE=

# ChatApps configuration directory
# Default: {HOTPLEX_CONFIG_DIR}/configs
# CHATAPPS_CONFIG_DIR=/path/to/configs
```

**Step 2: 更新 Docker 部署文档**

在 `docs/docker-deployment_zh.md` 中添加配置章节：

```markdown
## 配置管理

### 目录结构

```
~/.hotplex/
├── .env                    # 敏感配置
└── configs/                # 平台配置
    ├── slack.yaml
    └── ...
```

### Docker 部署

```bash
# 方式 1: 挂载用户目录
docker run -v ~/.hotplex:/root/.hotplex hotplex:latest

# 方式 2: 指定配置目录
docker run -e HOTPLEX_CONFIG_DIR=/app/config \
           -v ./configs:/app/configs \
           hotplex:latest
```
```

**Step 3: Commit**

```bash
git add .env.example docs/docker-deployment_zh.md
git commit -m "docs: add config directory documentation"
```

---

## Task 4: 验证部署场景

**Step 1: 测试 Docker 场景**

```bash
# 创建测试配置目录
mkdir -p /tmp/hotplex-test/configs
cp chatapps/configs/slack.yaml /tmp/hotplex-test/configs/

# 验证加载
# (需要运行程序并检查日志输出)
```

**Step 2: 验证向后兼容**

```bash
# 确保 CHATAPPS_CONFIG_DIR 仍然有效
CHATAPPS_CONFIG_DIR=/custom/path go run ./cmd/hotplexd
```

---

## 验收标准

- [ ] `~/.hotplex/configs/` 目录存在时自动加载
- [ ] `CHATAPPS_CONFIG_DIR` 环境变量优先级最高（向后兼容）
- [ ] 无配置时 fallback 到 `./chatapps/configs/`
- [ ] Docker 挂载 `~/.hotplex` 可生效
- [ ] 文档更新完成

---

**Plan complete and saved to `docs/plans/2026-03-04-config-deployment-design.md`.**

Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
