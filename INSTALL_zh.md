<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex 横幅"/>

  # HotPlex 安装指南
</div>


本文档介绍如何在各种平台上安装和配置 HotPlex。

[English](INSTALL.md)

## 快速开始

### 一键安装 (Linux / macOS / WSL)

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
```

### 指定版本安装

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -v v0.22.0
```

> 说明：`--` 用于分隔 bash 参数和脚本参数。没有它，`-v` 会被 bash 解析而非脚本。

### 自定义安装目录

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -d ~/bin
```

### 干运行模式

```bash
# 预览安装操作而不实际执行
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -n
```

### 强制重新安装

```bash
# 覆盖已安装的相同版本
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -f
```

### 详细输出

```bash
# 显示详细调试信息
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -V
```

## 系统要求

| 平台    | 架构                                 | 支持 |
| ------- | ------------------------------------ | ---- |
| Linux   | amd64, arm64                         | ✅    |
| macOS   | amd64 (Intel), arm64 (Apple Silicon) | ✅    |
| Windows | WSL2                                 | ✅    |

**依赖项**:
- `curl` 或 `wget`
- `tar` (Linux/macOS) 或 `unzip` (Windows)

## 安装选项

| 选项                | 说明                              |
| ------------------- | --------------------------------- |
| `-v, --version`     | 指定版本 (默认: 最新)             |
| `-d, --dir`         | 安装目录 (默认: `/usr/local/bin`) |
| `-c, --config`      | 仅生成配置文件                    |
| `-u, --uninstall`   | 卸载 HotPlex                      |
| `-f, --force`       | 强制重新安装                      |
| `-n, --dry-run`     | 干运行模式，显示将执行的操作      |
| `-q, --quiet`       | 静默模式                          |
| `-V, --verbose`     | 详细输出                          |
| `--skip-verify`     | 跳过校验和验证                    |
| `--skip-wizard`     | 跳过安装后配置向导                |
| `--non-interactive` | 非交互模式                        |
| `-h, --help`        | 显示帮助                          |
| `--version`         | 显示脚本版本                      |

## 手动安装

### 1. 下载二进制文件

从 [Releases](https://github.com/hrygo/hotplex/releases) 页面下载对应平台的压缩包：

```bash
# Linux amd64
curl -LO https://github.com/hrygo/hotplex/releases/download/v0.22.0/hotplex_0.22.0_linux_amd64.tar.gz

# macOS arm64 (Apple Silicon)
curl -LO https://github.com/hrygo/hotplex/releases/download/v0.22.0/hotplex_0.22.0_darwin_arm64.tar.gz
```

### 2. 解压并安装

```bash
tar -xzf hotplex_0.22.0_linux_amd64.tar.gz
sudo mv hotplexd /usr/local/bin/
sudo chmod +x /usr/local/bin/hotplexd
```

### 3. 验证安装

```bash
hotplexd -version
```

## 配置

### 生成配置模板

```bash
# 仅生成配置文件
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -c
```

配置文件默认生成在 `~/.config/hotplex/.env` (XDG 标准路径)

### 必要配置项

编辑 `~/.config/hotplex/.env`，填写以下必要配置：

```bash
# API 安全令牌 (生产环境必填)
HOTPLEX_API_KEY=your-secure-api-key

# Slack Bot 配置
HOTPLEX_SLACK_PRIMARY_OWNER=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_TOKEN=xoxb-your-token
HOTPLEX_SLACK_APP_TOKEN=xapp-your-token

# GitHub Token (用于 Git 操作)
GITHUB_TOKEN=ghp_your-token
```

### 配置文件搜索优先级

HotPlex 遵循 XDG 规范，按以下顺序查找配置文件：

1. `--env-file` 参数指定的路径
2. `ENV_FILE` 环境变量指定的路径
3. 当前执行目录下的 `.env`
4. `~/.config/hotplex/.env` (macOS/Linux 标准路径)

## 启动服务

```bash
# 使用默认配置 (自动搜索 .env 和 server.yaml)
hotplexd

# 显式指定配置文件
hotplexd --env-file ~/.config/hotplex/.env --config ~/.config/hotplex/server.yaml

# 指定 ChatApps 配置目录
hotplexd --config-dir ~/.config/hotplex/configs

# 查看帮助
hotplexd --help
```

## Docker 部署

```bash
# 拉取镜像 (根据需求选择: base, node, python, rust, java, 或包含全部环境的 full)
docker pull ghcr.io/hrygo/hotplex:node

# 运行容器
docker run -d \
  --name hotplex \
  -p 8080:8080 \
  -v ~/.config/hotplex:/root/.hotplex \
  -v ~/projects:/root/projects \
  ghcr.io/hrygo/hotplex:node
```

## 卸载

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -u
```

或手动删除：

```bash
sudo rm /usr/local/bin/hotplexd
# 可选：删除配置
rm -rf ~/.config/hotplex
```

## 故障排除

### 权限问题

如果安装到 `/usr/local/bin` 遇到权限问题：

```bash
# 使用 sudo 安装
curl -sL ... | sudo bash

# 或安装到用户目录
curl -sL ... | bash -s -- -d ~/.local/bin
```

### 找不到命令

确保安装目录在 `PATH` 中：

```bash
echo $PATH
# 如果没有，添加到 ~/.bashrc 或 ~/.zshrc
export PATH="$HOME/.local/bin:$PATH"
```

### 版本不匹配

清理缓存后重新下载：

```bash
rm -rf /tmp/hotplex-*
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
```

## 下一步

- [配置 Slack Bot](./configs/chatapps/slack.yaml)
- [查看 API 文档](./README_zh.md)
- [贡献指南](./CONTRIBUTING.md)

---

<div align="center">
  <img src="docs/images/logo.svg" alt="HotPlex 图标" width="100"/>
  <br/>
  <sub>为 AI 工程化社区倾力构建</sub>
</div>

