#!/bin/bash
#
# HotPlex Service Manager
# Install and manage hotplexd as a system service
#
# Usage:
#   ./scripts/service.sh install   - Install as system service
#   ./scripts/service.sh uninstall - Remove system service
#   ./scripts/service.sh start     - Start service
#   ./scripts/service.sh stop      - Stop service
#   ./scripts/service.sh restart   - Restart service
#   ./scripts/service.sh status    - Check service status
#   ./scripts/service.sh logs      - Tail service logs
#   ./scripts/service.sh enable    - Enable auto-start on boot
#   ./scripts/service.sh disable   - Disable auto-start on boot
#
# Supported systems:
#   - macOS: launchd (LaunchAgent)
#   - Linux: systemd

set -e

# --- Configuration ---
BINARY_NAME="hotplexd"
SERVICE_NAME="com.hotplex.daemon"
DISPLAY_NAME="HotPlex Daemon"

# Detect project root (for building only)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Installation Directories (independent of project) ---
if [[ "$(uname -s)" == "Darwin" ]]; then
    # macOS: Use user-local paths
    INSTALL_BIN="$HOME/.local/bin"
    INSTALL_ETC="$HOME/.config/hotplex"
    INSTALL_LOG="$HOME/.local/share/hotplex/logs"
else
    # Linux: Use system paths (requires sudo)
    INSTALL_BIN="/usr/local/bin"
    INSTALL_ETC="/etc/hotplex"
    INSTALL_LOG="/var/log/hotplex"
fi

# Installed paths
BINARY_PATH="$INSTALL_BIN/$BINARY_NAME"
ENV_FILE="$INSTALL_ETC/.env"
LOG_FILE="$INSTALL_LOG/daemon.log"

# Source paths (for installation)
SOURCE_BINARY="$PROJECT_ROOT/dist/$BINARY_NAME"
SOURCE_ENV="$PROJECT_ROOT/.env"
SOURCE_ENV_EXAMPLE="$PROJECT_ROOT/.env.example"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- System Detection ---
detect_system() {
    case "$(uname -s)" in
        Darwin*) echo "macos" ;;
        Linux*)  echo "linux" ;;
        *)       echo "unsupported" ;;
    esac
}

SYSTEM=$(detect_system)

# --- Helper Functions ---
info() { printf "${CYAN}ℹ️  %s${NC}\n" "$1"; }
success() { printf "${GREEN}✅ %s${NC}\n" "$1"; }
warn() { printf "${YELLOW}⚠️  %s${NC}\n" "$1"; }
error() { printf "${RED}❌ %s${NC}\n" "$1"; exit 1; }

check_source_binary() {
    if [[ ! -x "$SOURCE_BINARY" ]]; then
        error "Source binary not found: $SOURCE_BINARY\nRun 'make build' first."
    fi
}

check_installed_binary() {
    if [[ ! -x "$BINARY_PATH" ]]; then
        error "HotPlex not installed. Run 'make service-install' first."
    fi
}

ensure_dirs() {
    mkdir -p "$INSTALL_BIN" "$INSTALL_ETC" "$INSTALL_LOG"
}

install_files() {
    ensure_dirs

    info "Installing binary to $BINARY_PATH..."
    cp "$SOURCE_BINARY" "$BINARY_PATH"
    chmod +x "$BINARY_PATH"

    # Install .env (Update if exists to ensure prefixes are correct)
    if [[ -f "$ENV_FILE" ]]; then
        info "Backing up existing .env to .env.bak..."
        cp "$ENV_FILE" "$ENV_FILE.bak"
    fi

    if [[ -f "$SOURCE_ENV" ]]; then
        info "Updating config from $SOURCE_ENV to $ENV_FILE..."
        cp "$SOURCE_ENV" "$ENV_FILE"
    elif [[ -f "$SOURCE_ENV_EXAMPLE" ]]; then
        info "Creating config from example..."
        cp "$SOURCE_ENV_EXAMPLE" "$ENV_FILE"
        warn "Please edit $ENV_FILE with your settings"
    fi

    # Install server.yaml if not exists
    SERVER_YAML="$INSTALL_ETC/server.yaml"
    SOURCE_SERVER_YAML="$PROJECT_ROOT/configs/server.yaml"
    if [[ ! -f "$SERVER_YAML" ]] && [[ -f "$SOURCE_SERVER_YAML" ]]; then
        info "Installing server config to $SERVER_YAML..."
        cp "$SOURCE_SERVER_YAML" "$SERVER_YAML"
    fi

    # Install ChatApps configs
    INSTALL_CONFIGS="$INSTALL_ETC/configs"
    SOURCE_CONFIGS="$PROJECT_ROOT/configs/chatapps"
    if [[ -d "$SOURCE_CONFIGS" ]]; then
        info "Installing ChatApps configs to $INSTALL_CONFIGS..."
        mkdir -p "$INSTALL_CONFIGS"
        cp -r "$SOURCE_CONFIGS"/* "$INSTALL_CONFIGS/"
    fi
}

uninstall_files() {
    info "Removing installed files..."

    # Stop service first
    if [[ "$(uname -s)" == "Darwin" ]]; then
        launchctl stop "$SERVICE_NAME" 2>/dev/null || true
        launchctl unload "$PLIST_PATH" 2>/dev/null || true
    else
        sudo systemctl stop hotplexd 2>/dev/null || true
    fi

    # Remove binary
    rm -f "$BINARY_PATH"

    # Ask about config and logs
    read -p "Remove config directory ($INSTALL_ETC)? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$INSTALL_ETC"
    fi

    read -p "Remove log directory ($INSTALL_LOG)? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$INSTALL_LOG"
    fi

    success "Files removed"
}

# --- macOS launchd Functions ---
generate_plist() {
    cat << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${SERVICE_NAME}</string>
    <key>DisplayName</key>
    <string>${DISPLAY_NAME}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${BINARY_PATH}</string>
        <string>--config</string>
        <string>${INSTALL_ETC}/server.yaml</string>
        <string>--env-file</string>
        <string>${ENV_FILE}</string>
        <string>--config-dir</string>
        <string>${INSTALL_ETC}/configs</string>
    </array>
    <key>WorkingDirectory</key>
    <string>${INSTALL_ETC}</string>
    <key>StandardOutPath</key>
    <string>${LOG_FILE}</string>
    <key>StandardErrorPath</key>
    <string>${LOG_FILE}</string>
    <key>RunAtLoad</key>
    <false/>
    <key>KeepAlive</key>
    <false/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:${HOME}/.local/bin</string>
        <key>HOME</key>
        <string>${HOME}</string>
        <key>ENV_FILE</key>
        <string>${ENV_FILE}</string>
    </dict>
</dict>
</plist>
EOF
}

PLIST_PATH="$HOME/Library/LaunchAgents/${SERVICE_NAME}.plist"

macos_install() {
    check_source_binary

    info "Installing HotPlex as launchd service..."

    # Install files to system locations
    install_files

    # Generate and load plist
    generate_plist > "$PLIST_PATH"
    success "Created plist: $PLIST_PATH"

    # Reload to apply changes if already exists
    launchctl unload "$PLIST_PATH" 2>/dev/null || true
    launchctl load "$PLIST_PATH" 2>/dev/null || true
    success "Service loaded"

    echo ""
    printf "${BOLD}${CYAN}╭─ 📦 Installation Complete ─────────────────────────────${NC}\n"
    printf "  ${GREEN}✓${NC} Binary installed\n"
    printf "    ${CYAN}Path:${NC} $BINARY_PATH\n"
    printf "  ${GREEN}✓${NC} Configuration Files\n"
    printf "    ${CYAN}Main config:${NC}\n"
    printf "      ${CYAN}Path:${NC} $ENV_FILE\n"
    if [[ -f "$ENV_FILE" ]]; then
        printf "      ${GREEN}Status:${NC} Active\n"
    else
        printf "      ${YELLOW}Status:${NC} Please create this file\n"
    fi
    printf "    ${CYAN}ChatApps configs:${NC}\n"
    printf "      ${CYAN}Source:${NC} $PROJECT_ROOT/configs/chatapps/\n"
    for f in "$PROJECT_ROOT"/configs/chatapps/*.yaml; do
        if [[ -f "$f" ]]; then
            printf "      - $(basename "$f")\n"
        fi
    done
    printf "  ${GREEN}✓${NC} Logs\n"
    printf "    ${CYAN}Path:${NC} $LOG_FILE\n"
    printf "${BOLD}${CYAN}╰─────────────────────────────────────────────────────────${NC}\n"
    echo ""
    info "Run 'make service-start' to start the daemon"
}

macos_uninstall() {
    info "Uninstalling HotPlex service..."

    # Unload plist
    launchctl unload "$PLIST_PATH" 2>/dev/null || true
    rm -f "$PLIST_PATH"
    success "Service unloaded"

    # Remove installed files
    uninstall_files
    success "Uninstall complete"
}

macos_start() {
    check_installed_binary

    info "Starting HotPlex service..."
    launchctl start "$SERVICE_NAME" 2>/dev/null || {
        # Service might not be loaded, try loading first
        launchctl load "$PLIST_PATH" 2>/dev/null || true
        sleep 1
        launchctl start "$SERVICE_NAME"
    }
    success "Service started"

    printf "\n${BOLD}${CYAN}╭─ 🚀 Service Started ─────────────────────────────────${NC}\n"
    printf "  ${GREEN}✓${NC} Configuration Files in use\n"
    printf "    ${CYAN}Main config:${NC}\n"
    printf "      ${CYAN}Path:${NC} $ENV_FILE\n"
    printf "    ${CYAN}ChatApps configs:${NC}\n"
    printf "      ${CYAN}Source:${NC} $PROJECT_ROOT/configs/chatapps/\n"
    for f in "$PROJECT_ROOT"/configs/chatapps/*.yaml; do
        if [[ -f "$f" ]]; then
            printf "      - $(basename "$f")\n"
        fi
    done
    printf "  ${GREEN}✓${NC} Binary\n"
    printf "    ${CYAN}Path:${NC} $BINARY_PATH\n"
    printf "  ${GREEN}✓${NC} Logs\n"
    printf "    ${CYAN}Path:${NC} $LOG_FILE\n"
    printf "${BOLD}${CYAN}╰─────────────────────────────────────────────────────────${NC}\n"

    macos_status
}

macos_stop() {
    info "Stopping HotPlex service..."
    launchctl stop "$SERVICE_NAME" 2>/dev/null || true
    success "Service stopped"
}

macos_restart() {
    macos_stop
    sleep 1
    macos_start
}

macos_status() {
    if launchctl list "$SERVICE_NAME" &>/dev/null; then
        local pid
        # Use concise format: "PID\tExitCode\tLabel"
        pid=$(launchctl list | grep "^ *[0-9]*.*${SERVICE_NAME}" | grep -oE '^[0-9]+' || echo "")
        if [[ -n "$pid" && "$pid" != "0" ]]; then
            printf "${GREEN}✅ HotPlex service is running (PID: $pid)${NC}\n"
        else
            printf "${YELLOW}⏸️  HotPlex service is loaded but not running${NC}\n"
        fi
    else
        if [[ -f "$PLIST_PATH" ]]; then
            printf "${YELLOW}⏸️  HotPlex service is installed but not loaded${NC}\n"
        else
            printf "${RED}❌ HotPlex service is not installed${NC}\n"
        fi
    fi
}

macos_logs() {
    if [[ -f "$LOG_FILE" ]]; then
        tail -f "$LOG_FILE"
    else
        error "Log file not found: $LOG_FILE"
    fi
}

macos_enable() {
    info "Enabling auto-start on login..."

    # Update plist to run at load
    if [[ -f "$PLIST_PATH" ]]; then
        # Use PlistBuddy to update RunAtLoad
        /usr/libexec/PlistBuddy -c "Set :RunAtLoad true" "$PLIST_PATH" 2>/dev/null || \
            /usr/libexec/PlistBuddy -c "Add :RunAtLoad bool true" "$PLIST_PATH"

        # Reload the service
        launchctl unload "$PLIST_PATH" 2>/dev/null || true
        launchctl load "$PLIST_PATH"
        success "Auto-start enabled"
    else
        error "Service not installed. Run 'make service-install' first."
    fi
}

macos_disable() {
    info "Disabling auto-start on login..."

    if [[ -f "$PLIST_PATH" ]]; then
        /usr/libexec/PlistBuddy -c "Set :RunAtLoad false" "$PLIST_PATH" 2>/dev/null || true

        launchctl unload "$PLIST_PATH" 2>/dev/null || true
        launchctl load "$PLIST_PATH"
        success "Auto-start disabled"
    fi
}

# --- Linux systemd Functions ---
SERVICE_FILE="/etc/systemd/system/hotplexd.service"

generate_systemd_unit() {
    cat << EOF
[Unit]
Description=HotPlex Daemon - AI Agent Control Plane
Documentation=https://github.com/hrygo/hotplex
After=network.target

[Service]
Type=simple
User=${USER}
WorkingDirectory=${INSTALL_ETC}
ExecStart=${BINARY_PATH} --config ${INSTALL_ETC}/server.yaml --env-file ${ENV_FILE} --config-dir ${INSTALL_ETC}/configs
Restart=on-failure
RestartSec=5
StandardOutput=append:${LOG_FILE}
StandardError=append:${LOG_FILE}

# Environment
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
Environment="HOME=${HOME}"
Environment="ENV_FILE=${ENV_FILE}"

# Load .env file if exists
EnvironmentFile=-${ENV_FILE}

[Install]
WantedBy=multi-user.target
EOF
}

linux_install() {
    check_source_binary

    info "Installing HotPlex as systemd service..."

    # Create directories with proper permissions
    sudo mkdir -p "$INSTALL_BIN" "$INSTALL_ETC" "$INSTALL_LOG"
    sudo chown "$USER" "$INSTALL_ETC" "$INSTALL_LOG"

    # Install binary
    info "Installing binary to $BINARY_PATH..."
    sudo cp "$SOURCE_BINARY" "$BINARY_PATH"
    sudo chmod +x "$BINARY_PATH"

    # Install .env
    if [[ ! -f "$ENV_FILE" ]]; then
        if [[ -f "$SOURCE_ENV" ]]; then
            info "Installing config to $ENV_FILE..."
            sudo cp "$SOURCE_ENV" "$ENV_FILE"
            sudo chown "$USER" "$ENV_FILE"
        elif [[ -f "$SOURCE_ENV_EXAMPLE" ]]; then
            info "Creating config from example..."
            sudo cp "$SOURCE_ENV_EXAMPLE" "$ENV_FILE"
            sudo chown "$USER" "$ENV_FILE"
            warn "Please edit $ENV_FILE with your settings"
        fi
    else
        info "Config already exists at $ENV_FILE (preserved)"
    fi

    # Generate service file
    generate_systemd_unit | sudo tee "$SERVICE_FILE" > /dev/null
    success "Created service file: $SERVICE_FILE"

    sudo systemctl daemon-reload
    success "Systemd daemon reloaded"

    echo ""
    printf "${BOLD}${CYAN}╭─ 📦 Installation Complete ─────────────────────────────${NC}\n"
    printf "  ${GREEN}✓${NC} Binary installed\n"
    printf "    ${CYAN}Path:${NC} $BINARY_PATH\n"
    printf "  ${GREEN}✓${NC} Configuration Files\n"
    printf "    ${CYAN}Main config:${NC}\n"
    printf "      ${CYAN}Path:${NC} $ENV_FILE\n"
    if [[ -f "$ENV_FILE" ]]; then
        printf "      ${GREEN}Status:${NC} Active\n"
    else
        printf "      ${YELLOW}Status:${NC} Please create this file\n"
    fi
    printf "    ${CYAN}ChatApps configs:${NC}\n"
    printf "      ${CYAN}Source:${NC} $PROJECT_ROOT/configs/chatapps/\n"
    for f in "$PROJECT_ROOT"/configs/chatapps/*.yaml; do
        if [[ -f "$f" ]]; then
            printf "      - $(basename "$f")\n"
        fi
    done
    printf "  ${GREEN}✓${NC} Logs\n"
    printf "    ${CYAN}Path:${NC} $LOG_FILE\n"
    printf "${BOLD}${CYAN}╰─────────────────────────────────────────────────────────${NC}\n"
    echo ""
    info "Run 'sudo systemctl start hotplexd' to start the daemon"
}

linux_uninstall() {
    info "Uninstalling HotPlex service..."

    sudo systemctl stop hotplexd 2>/dev/null || true
    sudo systemctl disable hotplexd 2>/dev/null || true
    sudo rm -f "$SERVICE_FILE"
    sudo systemctl daemon-reload
    success "Service removed"

    # Remove binary
    sudo rm -f "$BINARY_PATH"

    # Ask about config and logs
    read -p "Remove config directory ($INSTALL_ETC)? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo rm -rf "$INSTALL_ETC"
    fi

    read -p "Remove log directory ($INSTALL_LOG)? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo rm -rf "$INSTALL_LOG"
    fi

    success "Uninstall complete"
}

linux_start() {
    check_installed_binary

    info "Starting HotPlex service..."
    sudo systemctl start hotplexd
    success "Service started"

    printf "\n${BOLD}${CYAN}╭─ 🚀 Service Started ─────────────────────────────────${NC}\n"
    printf "  ${GREEN}✓${NC} Configuration Files in use\n"
    printf "    ${CYAN}Main config:${NC}\n"
    printf "      ${CYAN}Path:${NC} $ENV_FILE\n"
    printf "    ${CYAN}ChatApps configs:${NC}\n"
    printf "      ${CYAN}Source:${NC} $PROJECT_ROOT/configs/chatapps/\n"
    for f in "$PROJECT_ROOT"/configs/chatapps/*.yaml; do
        if [[ -f "$f" ]]; then
            printf "      - $(basename "$f")\n"
        fi
    done
    printf "  ${GREEN}✓${NC} Binary\n"
    printf "    ${CYAN}Path:${NC} $BINARY_PATH\n"
    printf "  ${GREEN}✓${NC} Logs\n"
    printf "    ${CYAN}Path:${NC} $LOG_FILE\n"
    printf "${BOLD}${CYAN}╰─────────────────────────────────────────────────────────${NC}\n"

    linux_status
}

linux_stop() {
    info "Stopping HotPlex service..."
    sudo systemctl stop hotplexd
    success "Service stopped"
}

linux_restart() {
    info "Restarting HotPlex service..."
    sudo systemctl restart hotplexd
    success "Service restarted"

    printf "\n${BOLD}${CYAN}╭─ 🚀 Service Restarted ───────────────────────────────${NC}\n"
    printf "  ${GREEN}✓${NC} Configuration Files in use\n"
    printf "    ${CYAN}Main config:${NC}\n"
    printf "      ${CYAN}Path:${NC} $ENV_FILE\n"
    printf "    ${CYAN}ChatApps configs:${NC}\n"
    printf "      ${CYAN}Source:${NC} $PROJECT_ROOT/configs/chatapps/\n"
    for f in "$PROJECT_ROOT"/configs/chatapps/*.yaml; do
        if [[ -f "$f" ]]; then
            printf "      - $(basename "$f")\n"
        fi
    done
    printf "  ${GREEN}✓${NC} Binary\n"
    printf "    ${CYAN}Path:${NC} $BINARY_PATH\n"
    printf "  ${GREEN}✓${NC} Logs\n"
    printf "    ${CYAN}Path:${NC} $LOG_FILE\n"
    printf "${BOLD}${CYAN}╰─────────────────────────────────────────────────────────${NC}\n"

    linux_status
}

linux_status() {
    if systemctl is-active --quiet hotplexd 2>/dev/null; then
        sudo systemctl status hotplexd --no-pager
    elif [[ -f "$SERVICE_FILE" ]]; then
        printf "${YELLOW}⏸️  HotPlex service is installed but not running${NC}\n"
    else
        printf "${RED}❌ HotPlex service is not installed${NC}\n"
    fi
}

linux_logs() {
    sudo journalctl -u hotplexd -f --no-pager
}

linux_enable() {
    info "Enabling auto-start on boot..."
    sudo systemctl enable hotplexd
    success "Auto-start enabled"
}

linux_disable() {
    info "Disabling auto-start on boot..."
    sudo systemctl disable hotplexd
    success "Auto-start disabled"
}

# --- Main Command Dispatcher ---
dispatch() {
    local cmd="$1"

    case "$SYSTEM" in
        macos)
            case "$cmd" in
                install)   macos_install ;;
                uninstall) macos_uninstall ;;
                start)     macos_start ;;
                stop)      macos_stop ;;
                restart)   macos_restart ;;
                status)    macos_status ;;
                logs)      macos_logs ;;
                enable)    macos_enable ;;
                disable)   macos_disable ;;
                *)         error "Unknown command: $cmd" ;;
            esac
            ;;
        linux)
            case "$cmd" in
                install)   linux_install ;;
                uninstall) linux_uninstall ;;
                start)     linux_start ;;
                stop)      linux_stop ;;
                restart)   linux_restart ;;
                status)    linux_status ;;
                logs)      linux_logs ;;
                enable)    linux_enable ;;
                disable)   linux_disable ;;
                *)         error "Unknown command: $cmd" ;;
            esac
            ;;
        *)
            error "Unsupported system: $(uname -s)"
            ;;
    esac
}

show_usage() {
    cat << EOF
${BOLD}${CYAN}🔥 HotPlex Service Manager${NC}

${BOLD}Usage:${NC}
    ./scripts/service.sh <command>

${BOLD}Commands:${NC}
    install     Install hotplexd as a system service
    uninstall   Remove the system service
    start       Start the service
    stop        Stop the service
    restart     Restart the service
    status      Check service status
    logs        Tail service logs (Ctrl+C to stop)
    enable      Enable auto-start on boot/login
    disable     Disable auto-start on boot/login

${BOLD}System:${NC}         $SYSTEM ($([ "$SYSTEM" = "macos" ] && echo "launchd" || echo "systemd"))
${BOLD}Install Paths:${NC}
  Binary:        $BINARY_PATH
  Config:        $ENV_FILE
  Logs:          $LOG_FILE

${BOLD}Notes:${NC}
    - Service runs independently of project directory
    - Config (.env) is copied during install
    - Edit $ENV_FILE to change settings
EOF
}

# --- Entry Point ---
if [[ $# -eq 0 ]]; then
    show_usage
    exit 0
fi

dispatch "$1"
