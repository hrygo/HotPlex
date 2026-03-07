#!/bin/bash
# ==============================================================================
# HotPlex 一键安装脚本 v2.1
# ==============================================================================
# 用法:
#   curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
#   curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -v v0.21.0
#
# 参考: https://github.com/hrygo/hotplex/blob/main/INSTALL.md
# ==============================================================================

set -euo pipefail

# ==============================================================================
# 全局变量
# ==============================================================================
readonly REPO="hrygo/hotplex"
readonly BINARY_NAME="hotplexd"
readonly SCRIPT_VERSION="2.1.0"
readonly DEFAULT_INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="${HOME}/.hotplex"
readonly GITHUB_API="https://api.github.com/repos"
readonly LOG_FILE="${CONFIG_DIR}/install.log"

# 可配置变量
VERSION=""
INSTALL_DIR=""
CONFIG_ONLY=false
UNINSTALL=false
DRY_RUN=false
VERBOSE=false
QUIET=false
SKIP_VERIFY=false
SKIP_WIZARD=false
FORCE=false
INTERACTIVE=true

# 颜色定义 (非 readonly，允许 init_colors 修改)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# 临时文件
TEMP_DIR=""
CLEANUP_PENDING=true

# ==============================================================================
# 工具函数
# ==============================================================================

# 初始化颜色
init_colors() {
    if [[ ! -t 1 ]] || [[ "${NO_COLOR:-}" == "true" ]]; then
        RED="" GREEN="" YELLOW="" BLUE="" CYAN="" MAGENTA="" BOLD="" DIM="" NC=""
    fi
}

# 日志函数
log() {
    local level="$1"; shift
    local msg="$*"
    local timestamp=$(date '+%H:%M:%S')

    case "$level" in
        info)    [[ "$QUIET" == "true" ]] && return; echo -e "${BLUE}▸${NC} $msg" ;;
        success) [[ "$QUIET" == "true" ]] && return; echo -e "${GREEN}✓${NC} $msg" ;;
        warn)    echo -e "${YELLOW}!${NC} $msg" >&2 ;;
        error)   echo -e "${RED}✗${NC} $msg" >&2 ;;
        debug)   [[ "$VERBOSE" == "true" ]] && echo -e "${DIM}[DEBUG]${NC} $msg" ;;
        raw)     [[ "$QUIET" == "true" ]] && return; echo -e "$msg" ;;
        step)    [[ "$QUIET" == "true" ]] && return; echo -e "${CYAN}→${NC} $msg" ;;
    esac

    # 写入日志文件
    if [[ -d "$(dirname "$LOG_FILE")" ]] || mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null; then
        echo "[$timestamp] [$level] $msg" >> "$LOG_FILE" 2>/dev/null || true
    fi
}

info()    { log info "$*"; }
success() { log success "$*"; }
warn()    { log warn "$*"; }
error()   { log error "$*"; exit 1; }
debug()   { log debug "$*"; }
raw()     { log raw "$*"; }
step()    { log step "$*"; }

# 进度指示器
show_spinner() {
    local pid=$1
    local msg="$2"
    local spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    local i=0

    while kill -0 $pid 2>/dev/null; do
        i=$(( (i+1) % 10 ))
        printf "\r${CYAN}${spin:$i:1}${NC} ${msg}..."
        sleep 0.1
    done
    printf "\r"
}

# 清理函数
cleanup() {
    if [[ -n "$TEMP_DIR" ]] && [[ -d "$TEMP_DIR" ]] && [[ "$CLEANUP_PENDING" == "true" ]]; then
        rm -rf "$TEMP_DIR"
        debug "已清理临时目录: $TEMP_DIR"
    fi
}

# 错误处理
on_error() {
    local exit_code=$?
    local line_no=$1
    echo ""
    error "安装失败 (第 ${line_no} 行, 退出码: ${exit_code})"
    echo ""
    echo "  故障排除:"
    echo "    1. 检查网络连接"
    echo "    2. 使用 -V 查看详细日志"
    echo "    3. 查看日志: ${LOG_FILE}"
    echo ""
}

# 设置 trap
setup_traps() {
    trap cleanup EXIT
    trap 'on_error $LINENO' ERR
}

# 检查命令是否存在
command_exists() {
    command -v "$1" &>/dev/null
}

# 用户确认
confirm() {
    local prompt="$1"
    local default="${2:-n}"

    if [[ "$INTERACTIVE" != "true" ]] || [[ ! -t 0 ]]; then
        [[ "$default" == "y" ]] && return 0 || return 1
    fi

    local choices
    [[ "$default" == "y" ]] && choices="[Y/n]" || choices="[y/N]"

    echo -ne "${BOLD}?${NC} ${prompt} ${choices}: "
    read -r response
    response=${response:-$default}

    [[ "$response" =~ ^[Yy] ]]
}

# 用户输入
prompt_input() {
    local prompt="$1"
    local default="${2:-}"
    local secret="${3:-false}"

    if [[ "$INTERACTIVE" != "true" ]] || [[ ! -t 0 ]]; then
        echo "$default"
        return
    fi

    echo -ne "${BOLD}?${NC} ${prompt}"
    [[ -n "$default" ]] && echo -ne " [${default}]"
    echo -ne ": "

    if [[ "$secret" == "true" ]]; then
        read -rs response
        echo
    else
        read -r response
    fi

    echo "${response:-$default}"
}

# 检查依赖
check_dependencies() {
    local missing=()
    local optional_missing=()

    # 必需工具
    if ! command_exists curl && ! command_exists wget; then
        missing+=("curl 或 wget")
    fi

    if ! command_exists tar && ! command_exists unzip; then
        missing+=("tar 或 unzip")
    fi

    # 可选工具（用于向导）
    command_exists npm || optional_missing+=("npm (安装 Claude Code 需要)")
    command_exists jq || optional_missing+=("jq (API 验证)")
    command_exists openssl || optional_missing+=("openssl (生成密钥)")

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "缺少必需依赖: ${missing[*]}\n请安装后重试"
    fi

    if [[ ${#optional_missing[@]} -gt 0 ]] && [[ "$VERBOSE" == "true" ]]; then
        warn "可选依赖未安装: ${optional_missing[*]}"
    fi

    debug "依赖检查通过"
}

# 检测操作系统
detect_os() {
    local os
    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *) error "不支持的操作系统: $(uname -s)" ;;
    esac
    echo "$os"
}

# 检测架构
detect_arch() {
    local arch
    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) error "不支持的架构: $(uname -m)" ;;
    esac
    echo "$arch"
}

# HTTP 请求
http_get() {
    local url="$1"
    local output="${2:-}"

    debug "HTTP GET: $url"

    if command_exists curl; then
        local curl_opts=(-fsSL --connect-timeout 30 --max-time 300)
        [[ "$VERBOSE" == "true" ]] && curl_opts+=(-v)

        if [[ -n "$output" ]]; then
            curl "${curl_opts[@]}" -o "$output" "$url"
        else
            curl "${curl_opts[@]}" "$url"
        fi
    elif command_exists wget; then
        local wget_opts=(-q --timeout=30)
        [[ "$VERBOSE" == "true" ]] && wget_opts=()

        if [[ -n "$output" ]]; then
            wget "${wget_opts[@]}" -O "$output" "$url"
        else
            wget "${wget_opts[@]}" -O- "$url"
        fi
    fi
}

# 下载文件（带重试和进度）
download_with_retry() {
    local url="$1"
    local output="$2"
    local max_retries="${3:-3}"
    local retry=0

    while [[ $retry -lt $max_retries ]]; do
        debug "下载尝试 $((retry + 1))/$max_retries: $url"

        if http_get "$url" "$output"; then
            [[ -f "$output" ]] && [[ -s "$output" ]] && return 0
        fi

        retry=$((retry + 1))
        if [[ $retry -lt $max_retries ]]; then
            warn "下载失败，${retry}秒后重试..."
            sleep $retry
        fi
    done

    error "下载失败 (重试 $max_retries 次后): $url"
}

# 获取最新版本
get_latest_version() {
    local version

    # 方法1: GitHub API
    if command_exists curl; then
        version=$(curl -fsSL "${GITHUB_API}/${REPO}/releases/latest" 2>/dev/null | grep -oP '"tag_name":\s*"v?\K[^"]+' || true)
        [[ -n "$version" ]] && { echo "$version"; return 0; }
    fi

    # 方法2: 重定向解析
    version=$(http_get "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -oP 'tag/v?\K[^"]+' | head -1 || true)
    [[ -n "$version" ]] && { echo "$version"; return 0; }

    # 方法3: curl 头信息
    if command_exists curl; then
        version=$(curl -sIo- "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -i "location:" | sed -E 's/.*\/v?([^\/]+).*/\1/' | tr -d '\r' || true)
        [[ -n "$version" ]] && { echo "$version"; return 0; }
    fi

    return 1
}

# 获取已安装版本
get_installed_version() {
    local binary="${1:-${INSTALL_DIR}}/${BINARY_NAME}"

    if [[ -x "$binary" ]]; then
        "$binary" -version 2>/dev/null | head -1 | grep -oP 'v?\d+\.\d+\.\d+' || echo "unknown"
    fi
}

# 下载校验和文件
download_checksums() {
    local version="$1"
    local output="$2"
    local url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"

    debug "下载校验和: $url"
    http_get "$url" "$output" 2>/dev/null || return 1
    [[ -f "$output" ]] && [[ -s "$output" ]]
}

# 验证校验和
verify_checksum() {
    local archive="$1"
    local checksums_file="$2"
    local archive_name=$(basename "$archive")

    if ! command_exists sha256sum && ! command_exists shasum; then
        warn "无法验证校验和: 缺少 sha256sum 或 shasum"
        return 0
    fi

    debug "验证校验和: $archive_name"

    local expected checksum
    if command_exists sha256sum; then
        expected=$(grep "$archive_name" "$checksums_file" | awk '{print $1}')
        checksum=$(sha256sum "$archive" | awk '{print $1}')
    else
        expected=$(grep "$archive_name" "$checksums_file" | awk '{print $1}')
        checksum=$(shasum -a 256 "$archive" | awk '{print $1}')
    fi

    if [[ "$expected" == "$checksum" ]]; then
        debug "校验和验证通过"
        return 0
    else
        error "校验和验证失败!\n期望: $expected\n实际: $checksum"
    fi
}

# 备份现有安装
backup_existing() {
    local binary="${INSTALL_DIR}/${BINARY_NAME}"

    if [[ -f "$binary" ]]; then
        local backup="${CONFIG_DIR}/backups/${BINARY_NAME}.$(date +%Y%m%d%H%M%S)"
        mkdir -p "${CONFIG_DIR}/backups"
        info "备份现有安装..."
        cp "$binary" "$backup"
        success "备份保存到: $backup"
    fi
}

# 检查已安装版本
check_existing_installation() {
    local current_version
    current_version=$(get_installed_version)

    if [[ -n "$current_version" ]] && [[ "$current_version" != "unknown" ]]; then
        info "检测到已安装版本: ${GREEN}$current_version${NC}"

        if [[ "$FORCE" != "true" ]]; then
            if [[ "$VERSION" == "$current_version" ]] || [[ "$VERSION" == "v${current_version}" ]]; then
                warn "版本 $VERSION 已安装"
                if confirm "是否强制重新安装?" "n"; then
                    FORCE=true
                else
                    echo ""
                    info "使用 ${BINARY_NAME} -version 查看版本"
                    exit 0
                fi
            fi
        fi
    fi
}

# ==============================================================================
# 验证函数
# ==============================================================================

# 验证 Slack Token 格式
validate_slack_token() {
    local token="$1"
    [[ "$token" =~ ^xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+$ ]]
}

# 验证 Slack App Token 格式
validate_slack_app_token() {
    local token="$1"
    [[ "$token" =~ ^xapp-[0-9]+-[0-9]+-[a-zA-Z0-9]+$ ]]
}

# 验证 Slack User ID 格式
validate_slack_user_id() {
    local user_id="$1"
    [[ "$user_id" =~ ^[UBW][A-Z0-9]+$ ]]
}

# 验证 GitHub Token 格式
validate_github_token() {
    local token="$1"
    [[ "$token" =~ ^ghp_[a-zA-Z0-9]{36}$ ]] || [[ "$token" =~ ^github_pat_[a-zA-Z0-9_]+$ ]]
}

# 验证 Slack API 连接
test_slack_connection() {
    local token="$1"

    if ! command_exists curl; then
        warn "无法测试连接: 缺少 curl"
        return 0
    fi

    debug "测试 Slack API 连接..."

    local response
    response=$(curl -fsSL -H "Authorization: Bearer $token" \
        "https://slack.com/api/auth.test" 2>/dev/null || echo '{"ok":false}')

    if echo "$response" | grep -q '"ok":true'; then
        return 0
    else
        local error=$(echo "$response" | grep -oP '"error":\s*"\K[^"]+' || echo "unknown")
        debug "Slack API 错误: $error"
        return 1
    fi
}

# ==============================================================================
# 核心功能
# ==============================================================================

# 帮助信息
show_help() {
    cat << 'EOF'
HotPlex 一键安装脚本 v2.1

用法:
  curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
  install.sh [选项]

选项:
  -v, --version VERSION  指定安装版本 (默认: 最新版本)
  -d, --dir DIR          安装目录 (默认: /usr/local/bin)
  -c, --config           仅运行配置向导
  -u, --uninstall        卸载 HotPlex
  -f, --force            强制重新安装
  -n, --dry-run          干运行模式，显示将执行的操作
  -q, --quiet            静默模式
  -V, --verbose          详细输出
  --skip-verify          跳过校验和验证
  --skip-wizard          跳过安装后配置向导
  --non-interactive      非交互模式
  -h, --help             显示帮助信息
  --version              显示脚本版本

示例:
  install.sh                     # 安装最新版本 + 配置向导
  install.sh -v v0.21.0          # 安装指定版本
  install.sh -c                  # 仅运行配置向导
  install.sh -u                  # 卸载

环境变量:
  NO_COLOR=true                  禁用颜色输出

更多信息: https://github.com/hrygo/hotplex/blob/main/INSTALL.md
EOF
    exit 0
}

# 显示版本
show_version() {
    echo "HotPlex 安装脚本 v${SCRIPT_VERSION}"
    exit 0
}

# 解析参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -v|--version)     VERSION="$2"; shift 2 ;;
            -d|--dir)         INSTALL_DIR="$2"; shift 2 ;;
            -c|--config)      CONFIG_ONLY=true; shift ;;
            -u|--uninstall)   UNINSTALL=true; shift ;;
            -f|--force)       FORCE=true; shift ;;
            -n|--dry-run)     DRY_RUN=true; shift ;;
            -q|--quiet)       QUIET=true; shift ;;
            -V|--verbose)     VERBOSE=true; shift ;;
            --skip-verify)    SKIP_VERIFY=true; shift ;;
            --skip-wizard)    SKIP_WIZARD=true; shift ;;
            --non-interactive) INTERACTIVE=false; shift ;;
            -h|--help)        show_help ;;
            --version)        show_version ;;
            -*)               error "未知选项: $1\n使用 -h 查看帮助" ;;
            *)                break ;;
        esac
    done

    # 设置默认值
    INSTALL_DIR="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

    # 冲突检查
    [[ "$QUIET" == "true" ]] && [[ "$VERBOSE" == "true" ]] && warn "同时设置了 -q 和 -V，忽略 -q"
}

# 卸载
do_uninstall() {
    info "卸载 HotPlex..."
    local binary="${INSTALL_DIR}/${BINARY_NAME}"

    if [[ ! -f "$binary" ]]; then
        warn "HotPlex 未安装在 $binary"
        exit 0
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] 将删除: $binary"
        return
    fi

    # 检查是否在运行
    if pgrep -x "$BINARY_NAME" &>/dev/null; then
        warn "HotPlex 正在运行"
        if confirm "是否停止并卸载?" "y"; then
            pkill -x "$BINARY_NAME" 2>/dev/null || true
            sleep 1
        else
            exit 1
        fi
    fi

    if [[ -w "$INSTALL_DIR" ]]; then
        rm -f "$binary"
    else
        sudo rm -f "$binary"
    fi

    success "已删除: $binary"

    # 清理备份
    local backups="${CONFIG_DIR}/backups"
    if [[ -d "$backups" ]]; then
        local count=$(find "$backups" -name "${BINARY_NAME}.*" 2>/dev/null | wc -l)
        if [[ $count -gt 0 ]]; then
            info "发现 $count 个备份文件"
            if confirm "是否删除备份?" "n"; then
                rm -rf "$backups"
                success "已删除备份"
            fi
        fi
    fi

    if [[ -d "$CONFIG_DIR" ]]; then
        echo ""
        info "配置目录: $CONFIG_DIR"
        if confirm "是否删除配置目录?" "n"; then
            rm -rf "$CONFIG_DIR"
            success "已删除配置目录"
        fi
    fi

    success "卸载完成"
}

# ==============================================================================
# 配置向导
# ==============================================================================

# 向导：配置 Claude Code
wizard_claude_code() {
    echo ""
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    raw "${BOLD}  Step 1/2: Claude Code 配置${NC}"
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    # 检查 claude 是否已安装
    if command_exists claude; then
        local claude_version
        claude_version=$(claude --version 2>/dev/null | head -1 || echo "unknown")
        success "Claude Code 已安装: ${GREEN}$claude_version${NC}"
        echo ""
    else
        echo "  ${DIM}Claude Code 是 HotPlex 的推荐 AI 引擎${NC}"
        echo ""

        if command_exists npm; then
            if confirm "是否现在安装 Claude Code?" "y"; then
                step "正在安装 Claude Code..."
                if npm install -g @anthropic-ai/claude-code 2>/dev/null; then
                    success "Claude Code 安装成功"
                else
                    warn "安装失败，请手动执行: npm install -g @anthropic-ai/claude-code"
                fi
            fi
        else
            echo "  ${YELLOW}安装方法:${NC}"
            echo ""
            echo "    npm install -g @anthropic-ai/claude-code"
            echo ""
            echo "  ${DIM}或使用 Homebrew (macOS):${NC}"
            echo "    ${DIM}brew install claude${NC}"
            echo ""
        fi
    fi

    # 检查 ANTHROPIC_API_KEY
    if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
        success "ANTHROPIC_API_KEY 已设置"
    else
        echo ""
        warn "ANTHROPIC_API_KEY 未设置"
        echo ""

        if [[ "$INTERACTIVE" == "true" ]] && [[ -t 0 ]]; then
            local api_key
            api_key=$(prompt_input "请输入 ANTHROPIC_API_KEY" "" "true")

            if [[ -n "$api_key" ]]; then
                # 添加到 shell 配置文件
                local shell_rc=""
                if [[ -f "${HOME}/.zshrc" ]]; then
                    shell_rc="${HOME}/.zshrc"
                elif [[ -f "${HOME}/.bashrc" ]]; then
                    shell_rc="${HOME}/.bashrc"
                fi

                if [[ -n "$shell_rc" ]]; then
                    echo "" >> "$shell_rc"
                    echo "# Added by HotPlex installer" >> "$shell_rc"
                    echo "export ANTHROPIC_API_KEY='${api_key}'" >> "$shell_rc"
                    export ANTHROPIC_API_KEY="$api_key"
                    success "已添加到 $shell_rc"
                    info "请运行: source $shell_rc"
                fi
            fi
        else
            echo "  ${DIM}获取 API Key: https://console.anthropic.com/${NC}"
            echo ""
            echo "  设置方法 (添加到 ~/.bashrc 或 ~/.zshrc):"
            echo "    export ANTHROPIC_API_KEY='your-api-key'"
            echo ""
        fi
    fi

    # 代理配置
    echo ""
    echo -e "${CYAN}代理配置 (可选)${NC}"

    local current_proxy="${HTTPS_PROXY:-${HTTP_PROXY:-}}"
    if [[ -n "$current_proxy" ]]; then
        success "代理已配置: ${GREEN}$current_proxy${NC}"
        if confirm "是否修改代理设置?" "n"; then
            configure_proxy "$current_proxy"
        fi
    else
        if confirm "是否配置 HTTP 代理?" "n"; then
            configure_proxy ""
        fi
    fi
}

# 配置代理
configure_proxy() {
    local current_proxy="$1"
    local proxy_url

    echo ""
    echo "  ${DIM}代理地址格式: http://host:port 或 http://user:pass@host:port${NC}"
    echo "  ${DIM}示例: http://127.0.0.1:7890${NC}"
    echo ""

    proxy_url=$(prompt_input "请输入代理地址" "$current_proxy")

    if [[ -n "$proxy_url" ]]; then
        # 验证格式
        if [[ "$proxy_url" =~ ^https?:// ]]; then
            local shell_rc=""
            if [[ -f "${HOME}/.zshrc" ]]; then
                shell_rc="${HOME}/.zshrc"
            elif [[ -f "${HOME}/.bashrc" ]]; then
                shell_rc="${HOME}/.bashrc"
            fi

            if [[ -n "$shell_rc" ]]; then
                # 检查是否已存在代理配置
                if grep -q "HTTP_PROXY" "$shell_rc" 2>/dev/null; then
                    # 更新现有配置
                    sed -i.bak "s|export HTTP_PROXY=.*|export HTTP_PROXY='${proxy_url}'|" "$shell_rc"
                    sed -i.bak "s|export HTTPS_PROXY=.*|export HTTPS_PROXY='${proxy_url}'|" "$shell_rc"
                else
                    # 添加新配置
                    echo "" >> "$shell_rc"
                    echo "# Proxy settings (Added by HotPlex installer)" >> "$shell_rc"
                    echo "export HTTP_PROXY='${proxy_url}'" >> "$shell_rc"
                    echo "export HTTPS_PROXY='${proxy_url}'" >> "$shell_rc"
                fi

                export HTTP_PROXY="$proxy_url"
                export HTTPS_PROXY="$proxy_url"
                rm -f "${shell_rc}.bak"
                success "代理已配置: $proxy_url"
                info "请运行: source $shell_rc"
            fi
        else
            warn "代理地址格式无效，应以 http:// 或 https:// 开头"
        fi
    elif [[ -n "$current_proxy" ]]; then
        # 用户输入为空，询问是否清除
        if confirm "是否清除现有代理配置?" "n"; then
            local shell_rc=""
            if [[ -f "${HOME}/.zshrc" ]]; then
                shell_rc="${HOME}/.zshrc"
            elif [[ -f "${HOME}/.bashrc" ]]; then
                shell_rc="${HOME}/.bashrc"
            fi

            if [[ -n "$shell_rc" ]]; then
                sed -i.bak "/^export HTTP_PROXY=/d" "$shell_rc"
                sed -i.bak "/^export HTTPS_PROXY=/d" "$shell_rc"
                sed -i.bak "/^# Proxy settings/d" "$shell_rc"
                rm -f "${shell_rc}.bak"
                unset HTTP_PROXY
                unset HTTPS_PROXY
                success "代理配置已清除"
            fi
        fi
    fi
}

# 向导：配置 Slack Bot 凭据
wizard_slack_credentials() {
    local env_file="${CONFIG_DIR}/.env"

    echo ""
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    raw "${BOLD}  Step 2/3: Slack 凭据配置${NC}"
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    if [[ ! -f "$env_file" ]]; then
        warn "配置文件不存在，请先运行安装"
        return 1
    fi

    # 读取当前配置
    local current_bot_token=$(grep "^HOTPLEX_SLACK_BOT_TOKEN=" "$env_file" 2>/dev/null | cut -d'=' -f2- || echo "")
    local current_app_token=$(grep "^HOTPLEX_SLACK_APP_TOKEN=" "$env_file" 2>/dev/null | cut -d'=' -f2- || echo "")
    local current_user_id=$(grep "^HOTPLEX_SLACK_BOT_USER_ID=" "$env_file" 2>/dev/null | cut -d'=' -f2- || echo "")
    local current_github=$(grep "^GITHUB_TOKEN=" "$env_file" 2>/dev/null | cut -d'=' -f2- || echo "")

    # 检查配置状态
    local has_valid_slack=false
    [[ "$current_bot_token" =~ ^xoxb- ]] && has_valid_slack=true

    echo "  ${BOLD}当前配置状态:${NC}"
    echo ""
    echo "    Slack Bot Token:    $([[ "$current_bot_token" =~ ^xoxb- ]] && echo "${GREEN}✓ 已配置${NC}" || echo "${YELLOW}○ 未配置${NC}")"
    echo "    Slack App Token:    $([[ "$current_app_token" =~ ^xapp- ]] && echo "${GREEN}✓ 已配置${NC}" || echo "${YELLOW}○ 未配置${NC}")"
    echo "    Slack Bot User ID:  $([[ "$current_user_id" =~ ^[UBW][A-Z0-9]+$ ]] && echo "${GREEN}✓ 已配置${NC}" || echo "${YELLOW}○ 未配置${NC}")"
    echo "    GitHub Token:       $([[ "$current_github" =~ ^ghp_ ]] && echo "${GREEN}✓ 已配置${NC}" || echo "${YELLOW}○ 未配置${NC}")"
    echo ""

    # 如果都已配置，询问是否重新配置
    if [[ "$has_valid_slack" == "true" ]]; then
        if ! confirm "是否重新配置 Slack?" "n"; then
            success "Slack 配置保持不变"
            return 0
        fi
    fi

    echo "  ${BOLD}${CYAN}如何获取 Slack 凭据:${NC}"
    echo ""
    echo "  ${DIM}1. 访问${NC} ${UNDERLINE}https://api.slack.com/apps${NC}"
    echo "  ${DIM}2. 创建新 App 或选择现有 App${NC}"
    echo "  ${DIM}3. 启用 Socket Mode (推荐)${NC}"
    echo ""

    # 交互式配置
    if [[ "$INTERACTIVE" == "true" ]] && [[ -t 0 ]]; then
        local bot_token app_token user_id github_token
        local updated=false

        # Bot Token
        echo -e "${CYAN}Bot User OAuth Token (xoxb-...)${NC}"
        echo "  ${DIM}→ OAuth & Permissions → Bot User OAuth Token${NC}"
        bot_token=$(prompt_input "请输入" "$current_bot_token" "true")

        if [[ -n "$bot_token" ]]; then
            if validate_slack_token "$bot_token"; then
                # 测试连接
                step "验证 Token..."
                if test_slack_connection "$bot_token"; then
                    success "Token 验证成功"
                    sed -i.bak "s|^HOTPLEX_SLACK_BOT_TOKEN=.*|HOTPLEX_SLACK_BOT_TOKEN=${bot_token}|" "$env_file"
                    updated=true
                else
                    warn "Token 验证失败，但仍会保存"
                    sed -i.bak "s|^HOTPLEX_SLACK_BOT_TOKEN=.*|HOTPLEX_SLACK_BOT_TOKEN=${bot_token}|" "$env_file"
                    updated=true
                fi
            else
                warn "Token 格式无效 (应为 xoxb-...)"
            fi
        fi

        # App Token
        echo ""
        echo -e "${CYAN}App-Level Token (xapp-...)${NC}"
        echo "  ${DIM}→ Basic Information → App-Level Tokens${NC}"
        app_token=$(prompt_input "请输入" "$current_app_token" "true")

        if [[ -n "$app_token" ]]; then
            if validate_slack_app_token "$app_token"; then
                sed -i.bak "s|^HOTPLEX_SLACK_APP_TOKEN=.*|HOTPLEX_SLACK_APP_TOKEN=${app_token}|" "$env_file"
                updated=true
            else
                warn "Token 格式无效 (应为 xapp-...)"
            fi
        fi

        # Bot User ID
        echo ""
        echo -e "${CYAN}Bot User ID (U... 或 B...)${NC}"
        echo "  ${DIM}→ 点击机器人头像，查看 Member ID${NC}"
        user_id=$(prompt_input "请输入" "$current_user_id")

        if [[ -n "$user_id" ]]; then
            if validate_slack_user_id "$user_id"; then
                sed -i.bak "s|^HOTPLEX_SLACK_BOT_USER_ID=.*|HOTPLEX_SLACK_BOT_USER_ID=${user_id}|" "$env_file"
                updated=true
            else
                warn "User ID 格式无效 (应以 U、B 或 W 开头)"
            fi
        fi

        # GitHub Token
        echo ""
        if confirm "是否配置 GitHub Token?" "$([[ "$current_github" =~ ^ghp_ ]] && echo "n" || echo "y")"; then
            echo -e "${CYAN}GitHub Personal Access Token (ghp_...)${NC}"
            echo "  ${DIM}→ https://github.com/settings/tokens${NC}"
            github_token=$(prompt_input "请输入" "$current_github" "true")

            if [[ -n "$github_token" ]]; then
                if validate_github_token "$github_token"; then
                    sed -i.bak "s|^GITHUB_TOKEN=.*|GITHUB_TOKEN=${github_token}|" "$env_file"
                    updated=true
                else
                    warn "Token 格式无效 (应为 ghp_...)"
                fi
            fi
        fi

        # 清理备份
        rm -f "${env_file}.bak"

        if [[ "$updated" == "true" ]]; then
            success "配置已更新: $env_file"
        fi
    else
        # 非交互模式，显示指南
        echo "  ${BOLD}请手动编辑配置文件:${NC}"
        echo "    ${CONFIG_DIR}/.env"
        echo ""
    fi
}

# 向导：配置 Slack YAML
wizard_slack_yaml() {
    local yaml_file="${CONFIG_DIR}/slack.yaml"
    local yaml_source=""

    # 查找源配置文件
    if [[ -f "${INSTALL_DIR}/../chatapps/configs/slack.yaml" ]]; then
        yaml_source="${INSTALL_DIR}/../chatapps/configs/slack.yaml"
    elif [[ -f "/usr/local/share/hotplex/chatapps/configs/slack.yaml" ]]; then
        yaml_source="/usr/local/share/hotplex/chatapps/configs/slack.yaml"
    fi

    echo ""
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    raw "${BOLD}  Step 3/3: ChatApps 行为配置${NC}"
    raw "${BOLD}${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    # 复制默认配置（如果不存在）
    if [[ ! -f "$yaml_file" ]]; then
        if [[ -n "$yaml_source" ]] && [[ -f "$yaml_source" ]]; then
            mkdir -p "$(dirname "$yaml_file")"
            cp "$yaml_source" "$yaml_file"
            success "已创建配置文件: $yaml_file"
        else
            # 生成默认配置
            generate_slack_yaml "$yaml_file"
        fi
    else
        success "配置文件已存在: $yaml_file"
    fi

    echo ""
    echo "  ${BOLD}${CYAN}ChatApps 配置选项:${NC}"
    echo ""

    # 读取当前配置
    local current_work_dir=$(grep -E "^  work_dir:" "$yaml_file" 2>/dev/null | awk '{print $2}' || echo "~/projects")
    local current_mode=$(grep "^mode:" "$yaml_file" 2>/dev/null | awk '{print $2}' || echo "socket")
    local current_group_policy=$(grep -E "    group_policy:" "$yaml_file" 2>/dev/null | awk '{print $2}' || echo "multibot")
    local current_model=$(grep -E "  default_model:" "$yaml_file" 2>/dev/null | awk '{print $2}' || echo "sonnet")

    echo "  ${DIM}当前设置:${NC}"
    echo "    工作目录:     ${GREEN}$current_work_dir${NC}"
    echo "    连接模式:     ${GREEN}$current_mode${NC}"
    echo "    群组策略:     ${GREEN}$current_group_policy${NC}"
    echo "    AI 模型:      ${GREEN}$current_model${NC}"
    echo ""

    if ! confirm "是否修改 ChatApps 配置?" "n"; then
        return 0
    fi

    # 工作目录
    echo ""
    echo -e "${CYAN}工作目录 (work_dir)${NC}"
    echo "  ${DIM}Agent 执行代码的工作空间${NC}"
    local work_dir
    work_dir=$(prompt_input "请输入路径" "$current_work_dir")
    if [[ -n "$work_dir" ]] && [[ "$work_dir" != "$current_work_dir" ]]; then
        sed -i.bak "s|  work_dir:.*|  work_dir: ${work_dir}|" "$yaml_file"
        # 确保目录存在
        mkdir -p "$work_dir" 2>/dev/null || true
    fi

    # 连接模式
    echo ""
    echo -e "${CYAN}连接模式 (mode)${NC}"
    echo "  ${DIM}socket${NC} - 本地开发，无需公网 IP (推荐)"
    echo "  ${DIM}http${NC}   - 生产环境，使用 Webhook"
    echo ""
    local mode
    if confirm "使用 Socket Mode?" "y"; then
        mode="socket"
    else
        mode="http"
    fi
    if [[ "$mode" != "$current_mode" ]]; then
        sed -i.bak "s/^mode:.*/mode: ${mode}/" "$yaml_file"
    fi

    # 群组策略
    echo ""
    echo -e "${CYAN}群组响应策略 (group_policy)${NC}"
    echo "  ${DIM}allow${NC}     - 响应所有消息"
    echo "  ${DIM}mention${NC}   - 仅 @提及 时响应"
    echo "  ${DIM}multibot${NC}  - 多 Bot 模式，@提及时响应，无 @ 时广播提示"
    echo ""
    echo "  选择群组策略:"
    echo "    1) allow"
    echo "    2) mention"
    echo "    3) multibot (默认)"
    local policy_choice
    policy_choice=$(prompt_input "请选择 [1-3]" "3")
    local group_policy="multibot"
    case "$policy_choice" in
        1) group_policy="allow" ;;
        2) group_policy="mention" ;;
        3) group_policy="multibot" ;;
    esac
    if [[ "$group_policy" != "$current_group_policy" ]]; then
        sed -i.bak "s/    group_policy:.*/    group_policy: ${group_policy}/" "$yaml_file"
    fi

    # AI 模型
    echo ""
    echo -e "${CYAN}AI 模型 (default_model)${NC}"
    echo "  ${DIM}sonnet${NC} - 平衡性能与成本 (推荐)"
    echo "  ${DIM}haiku${NC}  - 快速响应，低成本"
    echo "  ${DIM}opus${NC}   - 最强性能，较高成本"
    echo ""
    echo "  选择 AI 模型:"
    echo "    1) sonnet (默认)"
    echo "    2) haiku"
    echo "    3) opus"
    local model_choice
    model_choice=$(prompt_input "请选择 [1-3]" "1")
    local model="sonnet"
    case "$model_choice" in
        1) model="sonnet" ;;
        2) model="haiku" ;;
        3) model="opus" ;;
    esac
    if [[ "$model" != "$current_model" ]]; then
        sed -i.bak "s/  default_model:.*/  default_model: ${model}/" "$yaml_file"
    fi

    # 清理备份
    rm -f "${yaml_file}.bak"

    echo ""
    success "ChatApps 配置已更新"
    echo ""
    echo "  ${DIM}完整配置文件: ${yaml_file}${NC}"
    echo "  ${DIM}配置文档: https://github.com/hrygo/hotplex/blob/main/docs/chatapps/chatapps-slack.md${NC}"
}

# 生成默认 Slack YAML 配置
generate_slack_yaml() {
    local yaml_file="$1"
    local work_dir="${HOME}/projects"

    mkdir -p "$(dirname "$yaml_file")"

    cat > "$yaml_file" << 'EOF'
# =============================================================================
# HotPlex Slack Adapter Configuration
# 由安装向导生成
# =============================================================================

platform: slack

# AI Provider
provider:
  type: claude-code
  enabled: true
  default_model: sonnet
  default_permission_mode: bypass-permissions
  dangerously_skip_permissions: true

# Engine
engine:
  work_dir: ~/projects
  timeout: 30m
  idle_timeout: 1h

# Session
session:
  timeout: 1h
  cleanup_interval: 5m

# Connection
mode: socket
server_addr: :8080

# AI Identity
system_prompt: |
  You are HotPlex, an expert software engineer in a Slack conversation.

  ## Environment
  - Running under HotPlex engine (stdin/stdout)
  - Headless mode - cannot prompt for user input

  ## Slack Context
  - Replies go to thread automatically
  - Keep answers concise - user expects quick responses

  ## Output
  - Be concise - short messages preferred
  - Use bullet lists over paragraphs
  - Use code blocks for code snippets
  - Avoid tables - use lists instead

task_instructions: |
  1. Understand before acting
  2. Avoid operations requiring user input
  3. Summarize tool output - don't dump raw data

# Features
features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true
  rate_limit:
    enabled: true
    max_attempts: 3
    base_delay_ms: 500
    max_delay_ms: 5000
  markdown:
    enabled: true

# Security
security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: multibot
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
    broadcast_response: |
      👋 Hello! I'm ready to help.
      Please @mention me if you'd like me to respond specifically to you.
    allowed_users: []
    blocked_users: []
    slash_command_rate_limit: 10.0
EOF

    # 替换工作目录
    sed -i "s|  work_dir: ~/projects|  work_dir: ${work_dir}|" "$yaml_file"

    success "已生成配置文件: $yaml_file"
}

# 运行安装向导
run_setup_wizard() {
    # 检查是否跳过向导
    if [[ "$SKIP_WIZARD" == "true" ]]; then
        debug "跳过配置向导"
        show_quick_start
        return 0
    fi

    # 检查是否是非交互模式
    if [[ ! -t 0 ]] || [[ "$QUIET" == "true" ]] || [[ "$DRY_RUN" == "true" ]]; then
        debug "非交互模式，跳过向导"
        return 0
    fi

    # 显示向导标题
    echo ""
    raw "${BOLD}════════════════════════════════════════════════════════════${NC}"
    raw "${BOLD}                    🧙 配置向导                              ${NC}"
    raw "${BOLD}════════════════════════════════════════════════════════════${NC}"

    wizard_claude_code
    wizard_slack_credentials
    wizard_slack_yaml

    # 完成提示
    echo ""
    raw "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    raw "${BOLD}  ✓ 配置完成${NC}"
    raw "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    show_quick_start
}

# 显示快速开始指南
show_quick_start() {
    echo ""
    raw "${GREEN}${BOLD}🎉 HotPlex 安装成功!${NC}"
    echo ""
    echo "  ${BOLD}快速开始:${NC}"
    echo ""
    echo "    1. 编辑配置 (如需要):"
    echo "       ${DIM}${CONFIG_DIR}/.env${NC}"
    echo ""
    echo "    2. 启动服务:"
    echo "       ${GREEN}${BINARY_NAME} -env ${CONFIG_DIR}/.env${NC}"
    echo ""
    echo "    3. 查看帮助:"
    echo "       ${DIM}${BINARY_NAME} -h${NC}"
    echo ""
    echo "  ${DIM}文档: https://github.com/hrygo/hotplex#readme${NC}"
    echo "  ${DIM}问题: https://github.com/hrygo/hotplex/issues${NC}"
    echo ""
}

# 生成配置文件
generate_config() {
    info "生成配置文件..."

    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] 将创建: ${CONFIG_DIR}/.env"
        return
    fi

    mkdir -p "$CONFIG_DIR"
    mkdir -p "${CONFIG_DIR}/projects"
    mkdir -p "${CONFIG_DIR}/backups"

    local env_file="${CONFIG_DIR}/.env"

    if [[ -f "$env_file" ]] && [[ "$FORCE" != "true" ]]; then
        warn "配置文件已存在: $env_file"
        if ! confirm "是否覆盖?" "n"; then
            return
        fi
    fi

    # 备份现有配置
    if [[ -f "$env_file" ]]; then
        cp "$env_file" "${env_file}.bak.$(date +%Y%m%d%H%M%S)"
    fi

    # 生成随机 API Key
    local api_key
    if command_exists openssl; then
        api_key=$(openssl rand -hex 32)
    else
        api_key="change-me-$(date +%s)-$$"
    fi

    cat > "$env_file" << EOF
# ==============================================================================
# HotPlex 环境配置
# 生成时间: $(date -Iseconds)
# 完整配置参考: https://github.com/hrygo/hotplex/blob/main/.env.example
# ==============================================================================

# 核心服务器
HOTPLEX_PORT=8080
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_LOG_FORMAT=text
HOTPLEX_API_KEY=${api_key}

# Provider 配置
HOTPLEX_PROVIDER_TYPE=claude-code
HOTPLEX_PROVIDER_MODEL=sonnet

# Slack Bot 配置 (必填)
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_TOKEN=xoxb-
HOTPLEX_SLACK_APP_TOKEN=xapp-

# 消息存储
HOTPLEX_MESSAGE_STORE_ENABLED=true
HOTPLEX_MESSAGE_STORE_TYPE=sqlite
HOTPLEX_MESSAGE_STORE_SQLITE_PATH=${CONFIG_DIR}/chatapp_messages.db

# GitHub Token (用于 Git 操作)
GITHUB_TOKEN=ghp-
EOF

    chmod 600 "$env_file"
    success "已生成配置文件: $env_file"
}

# 安装
do_install() {
    local os arch

    os=$(detect_os)
    arch=$(detect_arch)

    info "系统: $(uname -s) $(uname -m)"
    info "平台: ${os}/${arch}"

    # 获取/验证版本
    if [[ -z "$VERSION" ]]; then
        step "获取最新版本..."
        VERSION=$(get_latest_version) || error "无法获取最新版本，请使用 -v 指定"
        [[ "$VERSION" != v* ]] && VERSION="v${VERSION}"
    fi
    info "目标版本: ${GREEN}$VERSION${NC}"

    # 检查已安装版本
    check_existing_installation

    # 构建下载信息
    local archive_name="hotplex_${VERSION#v}_${os}_${arch}"
    [[ "$os" == "windows" ]] && archive_name="${archive_name}.zip" || archive_name="${archive_name}.tar.gz"
    local archive_url="https://github.com/${REPO}/releases/download/${VERSION}/${archive_name}"

    debug "下载地址: $archive_url"

    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] 将下载: $archive_url"
        info "[DRY-RUN] 将安装到: ${INSTALL_DIR}/${BINARY_NAME}"
        info "[DRY-RUN] 将生成配置: ${CONFIG_DIR}/.env"
        return
    fi

    # 创建临时目录
    TEMP_DIR=$(mktemp -d)
    debug "临时目录: $TEMP_DIR"

    # 备份现有安装
    backup_existing

    # 下载
    local archive_path="${TEMP_DIR}/${archive_name}"
    step "正在下载..."
    download_with_retry "$archive_url" "$archive_path"

    # 下载并验证校验和
    if [[ "$SKIP_VERIFY" != "true" ]]; then
        local checksums_path="${TEMP_DIR}/checksums.txt"
        if download_checksums "$VERSION" "$checksums_path"; then
            step "验证校验和..."
            verify_checksum "$archive_path" "$checksums_path"
        else
            warn "无法下载校验和文件，跳过验证"
        fi
    fi

    # 解压
    step "正在解压..."
    if [[ "$os" == "windows" ]]; then
        command_exists unzip || error "需要 unzip 来解压 .zip 文件"
        unzip -q "$archive_path" -d "$TEMP_DIR"
    else
        tar -xzf "$archive_path" -C "$TEMP_DIR"
    fi

    # 安装
    step "正在安装到 ${INSTALL_DIR}..."

    if [[ ! -w "$INSTALL_DIR" ]] && [[ ! -d "$INSTALL_DIR" ]]; then
        if [[ -w "$(dirname "$INSTALL_DIR")" ]]; then
            mkdir -p "$INSTALL_DIR"
        else
            sudo mkdir -p "$INSTALL_DIR"
        fi
    fi

    local binary_path="${TEMP_DIR}/${BINARY_NAME}"
    [[ -f "$binary_path" ]] || error "解压后未找到 ${BINARY_NAME}"

    if [[ -w "$INSTALL_DIR" ]]; then
        cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    else
        sudo cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    # 验证安装
    local installed_binary="${INSTALL_DIR}/${BINARY_NAME}"
    if [[ ! -x "$installed_binary" ]]; then
        error "安装验证失败: $installed_binary 不可执行"
    fi

    local installed_version
    installed_version=$("$installed_binary" -version 2>/dev/null | head -1 || echo "unknown")
    success "安装成功: ${GREEN}$installed_version${NC}"

    # 生成配置
    generate_config

    # 运行配置向导
    run_setup_wizard

    # 清理备份标记
    CLEANUP_PENDING=false
}

# ==============================================================================
# 主入口
# ==============================================================================

main() {
    init_colors
    setup_traps
    parse_args "$@"

    # 显示 banner
    if [[ "$QUIET" != "true" ]]; then
        echo ""
        raw "  ${BOLD}╔═══════════════════════════════════════════╗${NC}"
        raw "  ${BOLD}║${NC}         ${CYAN}HotPlex${NC} 安装程序 v${SCRIPT_VERSION}          ${BOLD}║${NC}"
        raw "  ${BOLD}║${NC}       AI Agent Control Plane            ${BOLD}║${NC}"
        raw "  ${BOLD}╚═══════════════════════════════════════════╝${NC}"
        echo ""
    fi

    # 创建日志目录
    mkdir -p "$CONFIG_DIR" 2>/dev/null || true

    # 卸载模式
    if [[ "$UNINSTALL" == "true" ]]; then
        do_uninstall
        exit 0
    fi

    # 仅配置模式
    if [[ "$CONFIG_ONLY" == "true" ]]; then
        if [[ ! -f "${CONFIG_DIR}/.env" ]]; then
            generate_config
        fi
        run_setup_wizard
        exit 0
    fi

    # 检查依赖
    check_dependencies

    # 安装
    do_install
}

main "$@"
