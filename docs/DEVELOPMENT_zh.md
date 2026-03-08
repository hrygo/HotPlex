# HotPlex 开发指南

> 本指南涵盖本地开发设置、测试和常用工作流。
> 架构详情请参阅 [architecture_zh.md](architecture_zh.md)。
>
> **中文版** | **[English](development.md)**

## 目录

- [前置要求](#前置要求)
- [快速开始](#快速开始)
- [构建](#构建)
- [测试](#测试)
- [代码质量](#代码质量)
- [配置](#配置)
- [调试](#调试)
- [常见任务](#常见任务)

---

## 前置要求

### 必需

| 工具 | 版本  | 用途       |
| ---- | ----- | ---------- |
| Go   | 1.25+ | 主语言     |
| Make | 任意  | 构建自动化 |
| Git  | 2.x   | 版本控制   |

### 可选

| 工具          | 用途         |
| ------------- | ------------ |
| golangci-lint | 高级代码检查 |
| Docker        | 容器构建     |
| claude CLI    | 本地 AI 测试 |

### 环境设置

```bash
# 克隆仓库
git clone https://github.com/hrygo/hotplex.git
cd hotplex

# 复制环境变量模板
cp .env.example .env

# 编辑凭证
vim .env
```

---

## 快速开始

### 常用命令

```bash
make help        # 显示所有可用命令
make build       # 构建守护进程
make run         # 构建并前台运行
make test        # 运行单元测试
make lint        # 运行代码检查
```

### 首次构建

```bash
# 安装依赖
go mod download

# 构建守护进程
make build

# 使用默认配置运行
make run
```

---

## 构建

### 开发构建

```bash
# 快速构建（无 lint）
go build -o dist/hotplexd ./cmd/hotplexd

# 标准构建（含 fmt, vet, tidy）
make build
```

### 跨平台构建

```bash
# 构建所有平台
make build-all

# 输出在 dist/:
# - hotplexd-linux-amd64
# - hotplexd-linux-arm64
# - hotplexd-darwin-amd64
# - hotplexd-darwin-arm64
# - hotplexd-windows-amd64.exe
```

### 带版本信息构建

```bash
# 版本自动从 git 标签获取
VERSION=v1.0.0 make build
```

---

## 测试

### 单元测试

```bash
# 快速单元测试（默认）
make test

# 详细输出
go test -v -short ./...

# 特定包
go test -v ./engine/...
```

### 竞态检测

```bash
# 启用竞态检测
make test-race

# 或直接运行
go test -v -race ./...
```

### 集成测试

```bash
# 重型集成测试
make test-integration

# 所有测试
make test-all
```

### CI 优化测试

```bash
# CI 优化（并行、超时）
make test-ci
```

### 编写测试

遵循以下约定：

```go
// 使用表驱动测试
func TestSessionPool(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"有效输入", "test", "test", false},
        {"空输入", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 测试实现
        })
    }
}
```

**测试指南：**
- Mock 重 I/O（使用 echo/cat 模拟 CLI）
- `go test -race` 必须通过
- 一个源文件对应一个测试文件：`foo.go` -> `foo_test.go`

---

## 代码质量

### 格式化

```bash
make fmt        # 格式化 Go 代码
go fmt ./...
```

### 静态检查

```bash
make vet        # 检查可疑构造
go vet ./...
```

### Lint

```bash
make lint       # 运行 golangci-lint
```

**注意：** Lint 错误（如 `unused`）表示未完成的集成。链接代码，不要删除它。

### 模块维护

```bash
make tidy       # 清理 go.mod
go mod tidy
```

---

## 配置

### 配置优先级

1. **命令行参数**（最高优先级）
2. **环境变量**（`.env` 文件）
3. **YAML 配置文件**（`chatapps/configs/*.yaml`）
4. **默认值**（最低优先级）

### 环境变量

```bash
# 核心服务器
HOTPLEX_PORT=8080
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_API_KEY=your-secret-key

# 引擎
HOTPLEX_EXECUTION_TIMEOUT=30m
HOTPLEX_IDLE_TIMEOUT=1h

# Provider
HOTPLEX_PROVIDER_TYPE=claude-code
HOTPLEX_PROVIDER_MODEL=sonnet

# Slack（示例）
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_SLACK_APP_TOKEN=xapp-...
```

### YAML 配置结构

```yaml
# chatapps/configs/slack.yaml
platform: slack

provider:
  type: claude-code
  default_model: sonnet

engine:
  work_dir: ~/projects/myproject
  timeout: 30m
  idle_timeout: 1h

features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true
```

### 热重载

配置文件会被监控变化。编辑 YAML 文件，守护进程会自动重载。

---

## 调试

### 启用调试日志

```bash
# 在 .env 中
HOTPLEX_LOG_LEVEL=DEBUG
HOTPLEX_LOG_FORMAT=text
```

### 查看日志

```bash
# 前台模式（日志输出到 stdout）
make run

# 后台模式
make restart
tail -f .logs/daemon.log
```

### 常见问题

| 问题                        | 解决方案                                         |
| --------------------------- | ------------------------------------------------ |
| "command not found: claude" | 安装 Claude CLI 或设置 `HOTPLEX_PROVIDER_BINARY` |
| "permission denied"         | 检查 `work_dir` 权限                             |
| 会话不持久                  | 检查 `idle_timeout` 设置                         |
| Slack 无响应                | 验证 `HOTPLEX_SLACK_BOT_USER_ID` 正确            |

---

## 常见任务

### 使用特定配置运行

```bash
# 使用 --config 参数（最高优先级）
hotplexd --config /path/to/configs

# 或通过环境变量
export HOTPLEX_CHATAPPS_CONFIG_DIR=/path/to/configs
hotplexd
```

### 服务管理

```bash
make service-install    # 安装为系统服务
make service-start      # 启动服务
make service-status     # 检查状态
make service-logs       # 查看日志
make service-stop       # 停止服务
make service-uninstall  # 移除服务
```

### Docker 开发

```bash
make docker-build       # 构建镜像（无缓存，确保最新二进制）
make docker-build-cache # 构建镜像（有缓存，快速迭代）
make docker-up          # 启动容器
make docker-logs        # 查看日志
make docker-down        # 停止容器
make docker-restart     # 重启并同步配置
```

### 清理构建产物

```bash
make clean              # 删除 dist/ 和清理 Go 缓存
```

---

## Git 工作流

### 分支命名

```
<type>/<issue-id>-short-description

# 示例:
feat/123-add-user-auth
fix/456-memory-leak
docs/789-update-readme
```

### 提交格式

遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

```
<type>(scope): description

# Types: feat, fix, refactor, docs, test, chore, wip
# 示例:
feat(auth): add OAuth login (Refs #123)
fix(pool): resolve memory leak (Refs #456)
wip: checkpoint for feature X
```

### 提交前检查

```bash
# 提交前运行
make lint test
```

---

## 相关文档

- [architecture_zh.md](architecture_zh.md) - 系统架构
- [configuration_zh.md](configuration_zh.md) - 配置参考
- [CONTRIBUTING.md](../CONTRIBUTING.md) - 贡献指南
- [sdk-guide_zh.md](sdk-guide_zh.md) - SDK 开发指南
