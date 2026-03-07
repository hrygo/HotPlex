#!/bin/bash
# ==============================================================================
# HotPlex 一键安装脚本
# ==============================================================================
# 用法: curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
# 或者: curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -v v0.21.0
# ==============================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
REPO="hrygo/hotplex"
BINARY_NAME="hotplexd"
DEFAULT_INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="${HOME}/.hotplex"
VERSION=""

# 打印函数
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 帮助信息
usage() {
    cat << EOF
HotPlex 一键安装脚本

用法: $0 [选项]

选项:
    -v, --version VERSION  指定版本 (默认: 最新版本)
    -d, --dir DIR          安装目录 (默认: /usr/local/bin)
    -c, --config           仅生成配置文件
    -u, --uninstall        卸载 HotPlex
    -h, --help             显示帮助信息

示例:
    $0                     # 安装最新版本
    $0 -v v0.21.0          # 安装指定版本
    $0 -d ~/bin            # 安装到指定目录
    $0 -c                  # 仅生成配置文件
EOF
    exit 0
}

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--version) VERSION="$2"; shift 2 ;;
        -d|--dir) INSTALL_DIR="$2"; shift 2 ;;
        -c|--config) CONFIG_ONLY=true; shift ;;
        -u|--uninstall) UNINSTALL=true; shift ;;
        -h|--help) usage ;;
        *) error "未知参数: $1" ;;
    esac
done

INSTALL_DIR="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

# 检测操作系统
detect_os() {
    case "$(uname -s)" in
        Linux*)  OS="linux" ;;
        Darwin*) OS="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
        *) error "不支持的操作系统: $(uname -s)" ;;
    esac
    info "检测到操作系统: $OS"
}

# 检测架构
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) error "不支持的架构: $(uname -m)" ;;
    esac
    info "检测到架构: $ARCH"
}

# 检查命令是否存在
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# 获取最新版本
get_latest_version() {
    if command_exists curl; then
        curl -sI "https://github.com/${REPO}/releases/latest" 2>/dev/null | \
            grep -i "location:" | \
            sed -E 's/.*\/v([^\/]+).*/v\1/'
    elif command_exists wget; then
        wget -qO- "https://github.com/${REPO}/releases/latest" 2>/dev/null | \
            grep -oP 'tag/v\K[^"]+' | head -1
    else
        error "需要 curl 或 wget"
    fi
}

# 下载文件
download_file() {
    local url="$1"
    local output="$2"

    if command_exists curl; then
        curl -fSL --progress-bar -o "$output" "$url"
    elif command_exists wget; then
        wget -q --show-progress -O "$output" "$url"
    else
        error "需要 curl 或 wget"
    fi
}

# 卸载
uninstall() {
    info "卸载 HotPlex..."

    if [[ -f "${INSTALL_DIR}/${BINARY_NAME}" ]]; then
        sudo rm -f "${INSTALL_DIR}/${BINARY_NAME}"
        success "已删除 ${INSTALL_DIR}/${BINARY_NAME}"
    fi

    if [[ -d "$CONFIG_DIR" ]]; then
        warn "配置目录 $CONFIG_DIR 已保留，如需删除请手动执行: rm -rf $CONFIG_DIR"
    fi

    success "卸载完成"
    exit 0
}

# 生成配置文件
generate_config() {
    info "生成配置文件..."

    mkdir -p "$CONFIG_DIR"

    # 生成 .env 文件
    if [[ ! -f "${CONFIG_DIR}/.env" ]]; then
        cat > "${CONFIG_DIR}/.env" << 'EOF'
# ==============================================================================
# HotPlex 环境配置
# ==============================================================================
# 完整配置参考: https://github.com/hrygo/hotplex/blob/main/.env.example

# 核心服务器
HOTPLEX_PORT=8080
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_LOG_FORMAT=text
HOTPLEX_API_KEY=请生成强令牌

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

# GitHub Token (用于 Git 操作)
GITHUB_TOKEN=ghp_在此处填写
EOF
        success "已生成 ${CONFIG_DIR}/.env"
    else
        warn "${CONFIG_DIR}/.env 已存在，跳过"
    fi

    # 创建工作目录
    mkdir -p "${CONFIG_DIR}/projects"

    success "配置文件生成完成"
}

# 安装
install() {
    info "开始安装 HotPlex..."

    # 获取版本
    if [[ -z "$VERSION" ]]; then
        VERSION=$(get_latest_version)
        if [[ -z "$VERSION" ]]; then
            error "无法获取最新版本，请使用 -v 指定版本"
        fi
        VERSION="v${VERSION}"
    fi
    info "安装版本: $VERSION"

    # 构建下载 URL
    local archive_name="hotplex_${VERSION#v}_${OS}_${ARCH}"
    if [[ "$OS" == "windows" ]]; then
        archive_name="${archive_name}.zip"
    else
        archive_name="${archive_name}.tar.gz"
    fi
    local download_url="https://github.com/${REPO}/releases/download/${VERSION}/${archive_name}"

    info "下载地址: $download_url"

    # 创建临时目录
    local tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    # 下载
    local archive_path="${tmp_dir}/${archive_name}"
    info "正在下载..."
    download_file "$download_url" "$archive_path"

    # 解压
    info "正在解压..."
    if [[ "$OS" == "windows" ]]; then
        unzip -q "$archive_path" -d "$tmp_dir"
    else
        tar -xzf "$archive_path" -C "$tmp_dir"
    fi

    # 安装
    info "正在安装到 ${INSTALL_DIR}..."
    if [[ ! -w "$INSTALL_DIR" ]]; then
        sudo mkdir -p "$INSTALL_DIR"
        sudo cp "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    else
        cp "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    # 生成配置
    generate_config

    # 完成
    echo ""
    success "🎉 HotPlex 安装成功!"
    echo ""
    echo "后续步骤:"
    echo "  1. 编辑配置文件: ${CONFIG_DIR}/.env"
    echo "  2. 启动服务: ${BINARY_NAME} -env ${CONFIG_DIR}/.env"
    echo "  3. 查看帮助: ${BINARY_NAME} -h"
    echo ""
    echo "文档: https://github.com/hrygo/hotplex#readme"
}

# 主函数
main() {
    echo ""
    echo "  ╔═══════════════════════════════════════════╗"
    echo "  ║         HotPlex 安装程序                  ║"
    echo "  ║     AI Agent Control Plane                ║"
    echo "  ╚═══════════════════════════════════════════╝"
    echo ""

    # 卸载模式
    if [[ "${UNINSTALL}" == "true" ]]; then
        uninstall
        exit 0
    fi

    # 仅配置模式
    if [[ "${CONFIG_ONLY}" == "true" ]]; then
        generate_config
        exit 0
    fi

    # 安装模式
    detect_os
    detect_arch
    install
}

main "$@"
