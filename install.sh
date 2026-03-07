#!/bin/bash
# ==============================================================================
# HotPlex 一键安装脚本 v2.0
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
readonly SCRIPT_VERSION="2.0.0"
readonly DEFAULT_INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="${HOME}/.hotplex"
readonly GITHUB_API="https://api.github.com/repos"

# 可配置变量
VERSION=""
INSTALL_DIR=""
CONFIG_ONLY=false
UNINSTALL=false
DRY_RUN=false
VERBOSE=false
QUIET=false
SKIP_VERIFY=false
FORCE=false

# 颜色定义
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly BOLD='\033[1m'
readonly NC='\033[0m'

# 临时文件
TEMP_DIR=""
CLEANUP_PENDING=true

# ==============================================================================
# 工具函数
# ==============================================================================

# 初始化日志
init_colors() {
    if [[ ! -t 1 ]] || [[ "${NO_COLOR:-}" == "true" ]]; then
        RED="" GREEN="" YELLOW="" BLUE="" CYAN="" BOLD="" NC=""
    fi
}

# 日志函数
log() {
    local level="$1"; shift
    local msg="$*"

    case "$level" in
        info)    [[ "$QUIET" == "true" ]] && return; echo -e "${BLUE}[INFO]${NC} $msg" ;;
        success) [[ "$QUIET" == "true" ]] && return; echo -e "${GREEN}[OK]${NC} $msg" ;;
        warn)    echo -e "${YELLOW}[WARN]${NC} $msg" >&2 ;;
        error)   echo -e "${RED}[ERROR]${NC} $msg" >&2 ;;
        debug)   [[ "$VERBOSE" == "true" ]] && echo -e "${CYAN}[DEBUG]${NC} $msg" ;;
        raw)     [[ "$QUIET" == "true" ]] && return; echo -e "$msg" ;;
    esac
}

info()    { log info "$*"; }
success() { log success "$*"; }
warn()    { log warn "$*"; }
error()   { log error "$*"; exit 1; }
debug()   { log debug "$*"; }
raw()     { log raw "$*"; }

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
    error "脚本在第 ${line_no} 行失败 (退出码: ${exit_code})"
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

# 检查依赖
check_dependencies() {
    local missing=()

    # 必需工具
    if ! command_exists curl && ! command_exists wget; then
        missing+=("curl 或 wget")
    fi

    if ! command_exists tar && ! command_exists unzip; then
        missing+=("tar 或 unzip")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "缺少依赖: ${missing[*]}\n请安装后重试"
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
        [[ "$QUIET" == "true" ]] && curl_opts+=(-s)

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

# 下载文件（带重试）
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
        [[ $retry -lt $max_retries ]] && sleep $((retry * 2))
    done

    error "下载失败 (重试 $max_retries 次后): $url"
}

# 获取最新版本
get_latest_version() {
    local version

    # 方法1: GitHub API
    if version=$(http_get "${GITHUB_API}/${REPO}/releases/latest" 2>/dev/null | grep -oP '"tag_name":\s*"v?\K[^"]+'); then
        [[ -n "$version" ]] && { echo "$version"; return 0; }
    fi

    # 方法2: 重定向解析
    if version=$(http_get "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -oP 'tag/v?\K[^"]+' | head -1); then
        [[ -n "$version" ]] && { echo "$version"; return 0; }
    fi

    # 方法3: curl 头信息
    if command_exists curl; then
        version=$(curl -sIo- "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -i "location:" | sed -E 's/.*\/v?([^\/]+).*/\1/' | tr -d '\r')
        [[ -n "$version" ]] && { echo "$version"; return 0; }
    fi

    return 1
}

# 获取已安装版本
get_installed_version() {
    local binary="${INSTALL_DIR}/${BINARY_NAME}"

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
        local backup="${binary}.bak.$(date +%Y%m%d%H%M%S)"
        info "备份现有安装到: $backup"

        if [[ -w "$INSTALL_DIR" ]]; then
            cp "$binary" "$backup"
        else
            sudo cp "$binary" "$backup"
        fi

        echo "$backup" > "${TEMP_DIR}/backup_path"
    fi
}

# 检查已安装版本
check_existing_installation() {
    local current_version
    current_version=$(get_installed_version)

    if [[ -n "$current_version" ]] && [[ "$current_version" != "unknown" ]]; then
        info "检测到已安装版本: $current_version"

        if [[ "$FORCE" != "true" ]]; then
            if [[ "$VERSION" == "$current_version" ]] || [[ "$VERSION" == "v${current_version}" ]]; then
                warn "版本 $VERSION 已安装。使用 -f 强制重新安装"
                exit 0
            fi
        fi
    fi
}

# ==============================================================================
# 核心功能
# ==============================================================================

# 帮助信息
show_help() {
    cat << 'EOF'
HotPlex 一键安装脚本 v2.0

用法:
  curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
  install.sh [选项]

选项:
  -v, --version VERSION  指定安装版本 (默认: 最新版本)
  -d, --dir DIR          安装目录 (默认: /usr/local/bin)
  -c, --config           仅生成配置文件
  -u, --uninstall        卸载 HotPlex
  -f, --force            强制重新安装
  -n, --dry-run          干运行模式，显示将执行的操作
  -q, --quiet            静默模式
  -V, --verbose          详细输出
  --skip-verify          跳过校验和验证
  -h, --help             显示帮助信息
  --version              显示脚本版本

示例:
  install.sh                     # 安装最新版本
  install.sh -v v0.21.0          # 安装指定版本
  install.sh -d ~/bin            # 安装到指定目录
  install.sh -c                  # 仅生成配置文件
  install.sh -u                  # 卸载
  install.sh -n                  # 干运行模式

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
            -h|--help)        show_help ;;
            --version)        show_version ;;
            -*)               error "未知选项: $1\n使用 -h 查看帮助" ;;
            *)                error "未知参数: $1" ;;
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
        warn "HotPlex 正在运行，请先停止服务"
        exit 1
    fi

    if [[ -w "$INSTALL_DIR" ]]; then
        rm -f "$binary"
    else
        sudo rm -f "$binary"
    fi

    success "已删除: $binary"

    # 清理旧备份
    local backups=$(find "$INSTALL_DIR" -name "${BINARY_NAME}.bak.*" 2>/dev/null | wc -l)
    if [[ $backups -gt 0 ]]; then
        info "发现 $backups 个备份文件，使用以下命令清理:"
        echo "  rm ${INSTALL_DIR}/${BINARY_NAME}.bak.*"
    fi

    if [[ -d "$CONFIG_DIR" ]]; then
        info "配置目录已保留: $CONFIG_DIR"
        info "如需删除: rm -rf $CONFIG_DIR"
    fi

    success "卸载完成"
}

# 生成配置文件
generate_config() {
    info "生成配置文件..."

    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] 将创建配置目录: $CONFIG_DIR"
        info "[DRY-RUN] 将生成: ${CONFIG_DIR}/.env"
        return
    fi

    mkdir -p "$CONFIG_DIR"
    mkdir -p "${CONFIG_DIR}/projects"

    local env_file="${CONFIG_DIR}/.env"

    if [[ -f "$env_file" ]] && [[ "$FORCE" != "true" ]]; then
        warn "配置文件已存在: $env_file (使用 -f 覆盖)"
        return
    fi

    # 生成随机 API Key
    local api_key
    if command_exists openssl; then
        api_key=$(openssl rand -hex 32)
    else
        api_key="change-me-$(date +%s)"
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
HOTPLEX_SLACK_BOT_TOKEN=xoxb-在此填入
HOTPLEX_SLACK_APP_TOKEN=xapp-在此填入

# 消息存储
HOTPLEX_MESSAGE_STORE_ENABLED=true
HOTPLEX_MESSAGE_STORE_TYPE=sqlite
HOTPLEX_MESSAGE_STORE_SQLITE_PATH=${CONFIG_DIR}/chatapp_messages.db

# GitHub Token (用于 Git 操作)
GITHUB_TOKEN=ghp_在此处填写
EOF

    chmod 600 "$env_file"
    success "已生成配置文件: $env_file"
    warn "请编辑配置文件并填写必要凭据!"
}

# 安装
do_install() {
    local os arch version archive_name archive_url archive_path checksums_path

    os=$(detect_os)
    arch=$(detect_arch)

    info "系统: $(uname -s) $(uname -m)"
    info "平台: ${os}/${arch}"

    # 获取/验证版本
    if [[ -z "$VERSION" ]]; then
        info "获取最新版本..."
        VERSION=$(get_latest_version) || error "无法获取最新版本，请使用 -v 指定"
        [[ "$VERSION" != v* ]] && VERSION="v${VERSION}"
    fi
    info "目标版本: $VERSION"

    # 检查已安装版本
    check_existing_installation

    # 构建下载信息
    archive_name="hotplex_${VERSION#v}_${os}_${arch}"
    if [[ "$os" == "windows" ]]; then
        archive_name="${archive_name}.zip"
    else
        archive_name="${archive_name}.tar.gz"
    fi
    archive_url="https://github.com/${REPO}/releases/download/${VERSION}/${archive_name}"

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
    archive_path="${TEMP_DIR}/${archive_name}"
    info "正在下载..."
    download_with_retry "$archive_url" "$archive_path"

    # 下载并验证校验和
    if [[ "$SKIP_VERIFY" != "true" ]]; then
        checksums_path="${TEMP_DIR}/checksums.txt"
        if download_checksums "$VERSION" "$checksums_path"; then
            verify_checksum "$archive_path" "$checksums_path"
        else
            warn "无法下载校验和文件，跳过验证"
        fi
    fi

    # 解压
    info "正在解压..."
    if [[ "$os" == "windows" ]]; then
        command_exists unzip || error "需要 unzip 来解压 .zip 文件"
        unzip -q "$archive_path" -d "$TEMP_DIR"
    else
        tar -xzf "$archive_path" -C "$TEMP_DIR"
    fi

    # 安装
    info "正在安装到 ${INSTALL_DIR}..."

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
    success "安装成功: $installed_version"

    # 生成配置
    generate_config

    # 完成
    echo ""
    raw "${GREEN}${BOLD}🎉 HotPlex 安装成功!${NC}"
    echo ""
    echo "后续步骤:"
    echo "  1. 编辑配置: ${CONFIG_DIR}/.env"
    echo "  2. 启动服务: ${BINARY_NAME} -env ${CONFIG_DIR}/.env"
    echo "  3. 查看帮助: ${BINARY_NAME} -h"
    echo ""
    echo "文档: https://github.com/hrygo/hotplex#readme"

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

    # 卸载模式
    if [[ "$UNINSTALL" == "true" ]]; then
        do_uninstall
        exit 0
    fi

    # 仅配置模式
    if [[ "$CONFIG_ONLY" == "true" ]]; then
        generate_config
        exit 0
    fi

    # 检查依赖
    check_dependencies

    # 安装
    do_install
}

main "$@"
