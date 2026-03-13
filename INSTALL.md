# HotPlex Installation Guide

This guide covers installation and configuration on various platforms.

[简体中文](INSTALL_zh.md)

## Quick Start

### One-Click Install (Linux / macOS / WSL)

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
```

### Install Specific Version

```bash
# Download and run with version flag
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -v v0.22.0
```

> Note: `--` separates bash options from script options. Without it, `-v` would be interpreted by bash instead of the script.

### Custom Install Directory

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -d ~/bin
```

### Dry Run Mode

```bash
# Preview actions without executing
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -n
```

### Force Reinstall

```bash
# Overwrite existing same version
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -f
```

### Verbose Output

```bash
# Show detailed debug info
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -V
```

## System Requirements

| Platform | Architecture | Support |
|----------|--------------|---------|
| Linux | amd64, arm64 | ✅ |
| macOS | amd64 (Intel), arm64 (Apple Silicon) | ✅ |
| Windows | WSL2 | ✅ |

**Dependencies**:
- `curl` or `wget`
- `tar` (Linux/macOS) or `unzip` (Windows)

## Install Options

| Option | Description |
|--------|-------------|
| `-v, --version` | Specify version (default: latest) |
| `-d, --dir` | Install directory (default: `/usr/local/bin`) |
| `-c, --config` | Generate config files only |
| `-u, --uninstall` | Uninstall HotPlex |
| `-f, --force` | Force reinstall |
| `-n, --dry-run` | Dry run mode, show actions without executing |
| `-q, --quiet` | Quiet mode |
| `-V, --verbose` | Verbose output |
| `--skip-verify` | Skip checksum verification |
| `--skip-wizard` | Skip post-install setup wizard |
| `--non-interactive` | Non-interactive mode |
| `-h, --help` | Show help |
| `--version` | Show script version |

## Manual Install

### 1. Download Binary

Download from [Releases](https://github.com/hrygo/hotplex/releases):

```bash
# Linux amd64
curl -LO https://github.com/hrygo/hotplex/releases/download/v0.22.0/hotplex_0.22.0_linux_amd64.tar.gz

# macOS arm64 (Apple Silicon)
curl -LO https://github.com/hrygo/hotplex/releases/download/v0.22.0/hotplex_0.22.0_darwin_arm64.tar.gz
```

### 2. Extract and Install

```bash
tar -xzf hotplex_0.22.0_linux_amd64.tar.gz
sudo mv hotplexd /usr/local/bin/
sudo chmod +x /usr/local/bin/hotplexd
```

### 3. Verify

```bash
hotplexd -version
```

## Configuration

### Generate Config Template

```bash
# Generate config files only
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -c
```

Config file is created by default at `~/.config/hotplex/.env` (XDG standard path)

### Required Settings

Edit `~/.config/hotplex/.env`:

```bash
# API security token (required for production)
HOTPLEX_API_KEY=your-secure-api-key

# Slack Bot config
HOTPLEX_SLACK_PRIMARY_OWNER=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_TOKEN=xoxb-your-token
HOTPLEX_SLACK_APP_TOKEN=xapp-your-token

# GitHub Token (for Git operations)
GITHUB_TOKEN=ghp_your-token
```


### Config File Search Order

HotPlex follows the XDG specification and searches for config in this order:

1. Path specified by `--env-file` flag
2. Path specified by `ENV_FILE` environment variable
3. `.env` in current directory
4. `~/.config/hotplex/.env` (standard path on macOS/Linux)

## Start Service

```bash
# Default config (auto-searches for .env and server.yaml)
hotplexd

# Specify config files explicitly
hotplexd --env-file ~/.config/hotplex/.env --config ~/.config/hotplex/server.yaml

# Specify ChatApps config directory
hotplexd --config-dir ~/.config/hotplex/configs

# Show help
hotplexd --help
```

## Docker Deployment

```bash
# Pull image (choose your stack: base, node, python, rust, java, or full)
docker pull ghcr.io/hrygo/hotplex:node

# Run container
docker run -d \
  --name hotplex \
  -p 8080:8080 \
  -v ~/.config/hotplex:/root/.hotplex \
  -v ~/projects:/root/projects \
  ghcr.io/hrygo/hotplex:node
```

## Uninstall

```bash
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -u
```

Or manual removal:

```bash
sudo rm /usr/local/bin/hotplexd
# Optional: remove config
rm -rf ~/.config/hotplex
```

## Troubleshooting

### Permission Issues

If installing to `/usr/local/bin` fails:

```bash
# Use sudo
curl -sL ... | sudo bash

# Or install to user directory
curl -sL ... | bash -s -- -d ~/.local/bin
```

### Command Not Found

Ensure install directory is in `PATH`:

```bash
echo $PATH
# Add to ~/.bashrc or ~/.zshrc if missing
export PATH="$HOME/.local/bin:$PATH"
```

### Version Mismatch

Clear cache and reinstall:

```bash
rm -rf /tmp/hotplex-*
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
```

## Next Steps

- [Configure Slack Bot](./configs/chatapps/slack.yaml)
- [API Documentation](./README.md)
- [Contributing Guide](./CONTRIBUTING.md)
