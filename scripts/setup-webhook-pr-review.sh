#!/usr/bin/env bash
# setup-webhook-pr-review.sh — 一键配置 Caddy 反代 + HotPlex Webhook（服务器侧）
#
# 用法: sudo bash scripts/setup-webhook-pr-review.sh
#
# 功能:
#   1. 前置检查
#   2. 生成 Webhook Secret
#   3. 备份并更新 Caddyfile（公网反代，仅 /api/webhook/github 可达）
#   4. 验证并重载 Caddy
#   5. 配置 HotPlex（.env + config.yaml）
#   6. 重启 HotPlex Gateway
#   7. Cron 降频（3min → 30min）
#   8. 端到端验证
#
# GitHub Webhook 注册请另行执行:
#   bash scripts/setup-github-webhook.sh <secret>
#   （需以 hrygo 身份登录 gh CLI）
#
# 前置条件:
#   - sudo 权限
#   - HotPlex Gateway 运行在 127.0.0.1:8888
#   - Caddy 已通过 systemd 管理
#
set -uo pipefail

# ─── 配置 ─────────────────────────────────────────────────────────────────────
PUBLIC_IP="43.106.12.60"
REPO="hrygo/hotplex"
GATEWAY_ADDR="127.0.0.1:8888"
WEBHOOK_PATH="/api/webhook/github"
CADDYFILE="/etc/caddy/Caddyfile"
CADDYFILE_BAK="/etc/caddy/Caddyfile.bak.$(date +%Y%m%d%H%M%S)"
HOTPLEX_HOME="/home/hotplex/.hotplex"
ENV_FILE="${HOTPLEX_HOME}/.env"
CONFIG_FILE="${HOTPLEX_HOME}/config.yaml"
CRON_JOB_NAME="pr-review-hotplex"
LOGNAME_USER=$(logname 2>/dev/null || echo hotplex)

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail()  { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }

run_as_user() { su - "$LOGNAME_USER" -c "$*"; }

# ─── Step 1: 前置检查 ─────────────────────────────────────────────────────────
info "Step 1/8: 前置检查..."

[ "$(id -u)" -eq 0 ] || fail "需要 sudo 权限运行: sudo bash $0"
command -v caddy >/dev/null || fail "caddy 未安装"
command -v openssl >/dev/null || fail "openssl 未安装"

# 检查 HotPlex 是否在运行
if ! pgrep -f "hotplex gateway" > /dev/null 2>&1; then
    warn "HotPlex Gateway 未运行，将在配置后启动"
fi

ok "前置检查通过"

# ─── Step 2: 生成 Webhook Secret ─────────────────────────────────────────────
info "Step 2/8: 生成 Webhook Secret..."
SECRET=$(openssl rand -hex 32)
info "Secret 已生成: ${SECRET:0:8}..."

# ─── Step 3: 备份并更新 Caddyfile ────────────────────────────────────────────
info "Step 3/8: 配置 Caddy 反代..."

# 备份
cp "$CADDYFILE" "$CADDYFILE_BAK"
ok "Caddyfile 已备份: ${CADDYFILE_BAK}"

# 检查是否已有公网 site block
if grep -q "${PUBLIC_IP}" "$CADDYFILE" 2>/dev/null; then
    warn "Caddyfile 中已存在 ${PUBLIC_IP} 配置，跳过追加"
else
    # 使用 tls internal（Let's Encrypt 不支持纯 IP）
    cat >> "$CADDYFILE" <<EOF

# ──────────────────────────────────────────────────────────────────────────────
# 公网 Webhook 端点（仅 /api/webhook/github 可达，用于 GitHub → HotPlex 触发）
# 使用 Caddy 内置 CA 自签名证书（Let's Encrypt 不支持纯 IP）
# ──────────────────────────────────────────────────────────────────────────────
${PUBLIC_IP} {
    tls internal

    handle /api/webhook/github {
        reverse_proxy ${GATEWAY_ADDR}
    }

    handle {
        respond 404
    }

    log {
        output file /var/log/caddy/webhook.log {
            roll_size 10mb
            roll_keep 5
        }
        format json
    }
}
EOF
    ok "公网 webhook site block 已追加到 Caddyfile"
fi

# ─── Step 4: 验证并重载 Caddy ────────────────────────────────────────────────
info "Step 4/8: 验证并重载 Caddy..."

if ! caddy validate --config "$CADDYFILE" 2>&1; then
    cp "$CADDYFILE_BAK" "$CADDYFILE"
    fail "Caddyfile 验证失败，已回滚备份"
fi
ok "Caddyfile 验证通过"

systemctl reload caddy || fail "Caddy 重载失败"
sleep 3

if ! systemctl is-active --quiet caddy; then
    cp "$CADDYFILE_BAK" "$CADDYFILE"
    systemctl reload caddy || true
    fail "Caddy 重载后未正常运行，已回滚备份"
fi
ok "Caddy 已重载并正常运行"

# 验证路由隔离
info "验证 Caddy 路由隔离..."
sleep 2

HTTP_ROOT=$(curl -sk -o /dev/null -w "%{http_code}" "https://${PUBLIC_IP}/" 2>/dev/null || echo "000")
HTTP_HOOK=$(curl -sk -o /dev/null -w "%{http_code}" -X POST "https://${PUBLIC_IP}${WEBHOOK_PATH}" 2>/dev/null || echo "000")
HTTP_WS=$(curl -sk -o /dev/null -w "%{http_code}" "https://${PUBLIC_IP}/ws" 2>/dev/null || echo "000")

[ "$HTTP_ROOT" = "404" ] && ok "根路径 → 404 ✓" || warn "根路径 → ${HTTP_ROOT}（期望 404）"
[ "$HTTP_WS" = "404" ] && ok "/ws 路径 → 404 ✓" || warn "/ws 路径 → ${HTTP_WS}（期望 404）"

if [ "$HTTP_HOOK" = "403" ] || [ "$HTTP_HOOK" = "400" ] || [ "$HTTP_HOOK" = "405" ]; then
    ok "Webhook 路径 → ${HTTP_HOOK}（可达，被应用层拦截）✓"
else
    warn "Webhook 路径 → ${HTTP_HOOK}（期望 403/400/405）"
fi

# ─── Step 5: 配置 HotPlex ────────────────────────────────────────────────────
info "Step 5/8: 配置 HotPlex..."

# 写入 .env（幂等：已有则更新值）
if grep -q "^HOTPLEX_WEBHOOK_SECRET=" "$ENV_FILE" 2>/dev/null; then
    sed -i "s|^HOTPLEX_WEBHOOK_SECRET=.*|HOTPLEX_WEBHOOK_SECRET=${SECRET}|" "$ENV_FILE"
    ok ".env 中 HOTPLEX_WEBHOOK_SECRET 已更新"
else
    echo "" >> "$ENV_FILE"
    echo "# GitHub Webhook Secret (PR Review)" >> "$ENV_FILE"
    echo "HOTPLEX_WEBHOOK_SECRET=${SECRET}" >> "$ENV_FILE"
    ok "HOTPLEX_WEBHOOK_SECRET 已写入 ${ENV_FILE}"
fi
chmod 600 "$ENV_FILE"

# 更新 config.yaml（幂等）
if grep -q "^webhook:" "$CONFIG_FILE" 2>/dev/null; then
    warn "config.yaml 中已有 webhook 配置，跳过"
else
    cat >> "$CONFIG_FILE" <<'YAML'

# ──────────────────────────────────────────────────────────────────────────────
# WEBHOOK
# ──────────────────────────────────────────────────────────────────────────────
webhook:
  enabled: true
  # Secret 通过环境变量 HOTPLEX_WEBHOOK_SECRET 注入（见 .env）
  secret: ""
  path: "/api/webhook/github"
  max_body_size: 1048576  # 1MB
YAML
    ok "webhook 配置已追加到 ${CONFIG_FILE}"
fi

# ─── Step 6: 重启 HotPlex Gateway ────────────────────────────────────────────
info "Step 6/8: 重启 HotPlex Gateway..."

if run_as_user "hotplex gateway restart --config ${CONFIG_FILE}" 2>/dev/null; then
    ok "HotPlex Gateway 已重启"
else
    warn "自动重启失败，请手动执行: hotplex gateway restart"
fi

sleep 5

# ─── Step 7: Cron 降频 ───────────────────────────────────────────────────────
info "Step 7/8: Cron 降频（3min → 30min）..."

if run_as_user "hotplex cron update ${CRON_JOB_NAME} --schedule 'every:30m'" 2>/dev/null; then
    ok "cron 频率已降为 30min 兜底"
else
    warn "cron 降频失败，请手动执行: hotplex cron update ${CRON_JOB_NAME} --schedule 'every:30m'"
fi

# ─── Step 8: 端到端验证 ──────────────────────────────────────────────────────
info "Step 8/8: 端到端验证..."

# 8a. 验证 gateway 日志
sleep 3
if run_as_user "hotplex gateway logs 2>/dev/null | tail -30" 2>/dev/null | grep -q "webhook handler registered"; then
    ok "Gateway 日志: webhook 路由已注册 ✓"
else
    warn "未在日志中找到 'webhook handler registered'（可能日志延迟）"
fi

# 8b. 验证 webhook 端点可达
HTTP_VERIFY=$(curl -sk -o /dev/null -w "%{http_code}" -X POST \
    -H "X-Hub-Signature-256: sha256=deadbeef" \
    -H "X-GitHub-Event: ping" \
    -d '{}' \
    "https://${PUBLIC_IP}${WEBHOOK_PATH}" 2>/dev/null || echo "000")
if [ "$HTTP_VERIFY" = "403" ]; then
    ok "Webhook 端点可达且签名验证生效（403 Forbidden）✓"
elif [ "$HTTP_VERIFY" = "000" ]; then
    warn "无法连接 Webhook 端点（检查 Caddy 是否正确代理）"
else
    info "Webhook 端点返回 ${HTTP_VERIFY}"
fi

# ─── 完成 ────────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════════════════"
ok "服务器侧 Webhook 配置完成！"
echo "════════════════════════════════════════════════════════════════════════════"
echo ""
echo "  公网端点:    https://${PUBLIC_IP}${WEBHOOK_PATH}"
echo "  TLS:         Caddy 内置 CA 自签名（绑定域名后可升级 Let's Encrypt）"
echo "  Caddyfile:   ${CADDYFILE}"
echo "  备份:        ${CADDYFILE_BAK}"
echo "  Secret:      ${ENV_FILE} (HOTPLEX_WEBHOOK_SECRET=${SECRET:0:8}...)"
echo "  Cron 兜底:   every:30m（从 every:3m 降频）"
echo ""
echo "⚠️  下一步: 在本地电脑注册 GitHub Webhook:"
echo "  bash scripts/setup-github-webhook.sh ${SECRET}"
echo ""
echo "回滚:"
echo "  sudo cp ${CADDYFILE_BAK} ${CADDYFILE} && sudo systemctl reload caddy"
echo ""
