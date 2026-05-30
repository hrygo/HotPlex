#!/usr/bin/env bash
# setup-github-webhook.sh — 在 hrygo/hotplex 仓库注册 GitHub Webhook
#
# 用法: bash scripts/setup-github-webhook.sh <webhook-secret>
#
# 前置条件:
#   - gh CLI 已以 hrygo 身份登录（或对 hrygo/hotplex 有 admin 权限的账号）
#   - 先运行 sudo bash scripts/setup-webhook-pr-review.sh 获取 Secret
#     （或手动生成: openssl rand -hex 32）
#
# 如需切换 gh 身份: gh auth login
#
set -uo pipefail

PUBLIC_IP="43.106.12.60"
REPO="hrygo/hotplex"
WEBHOOK_PATH="/api/webhook/github"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail()  { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }

# ─── 参数 ─────────────────────────────────────────────────────────────────────
SECRET="${1:-}"

if [ -z "$SECRET" ]; then
    info "未提供 Secret，自动生成..."
    SECRET=$(openssl rand -hex 32)
fi

info "Webhook Secret: ${SECRET:0:8}..."
info "请将此 Secret 写入服务器: echo 'HOTPLEX_WEBHOOK_SECRET=${SECRET}' >> ~/.hotplex/.env"
echo ""

# ─── 前置检查 ─────────────────────────────────────────────────────────────────
info "前置检查..."

command -v gh >/dev/null || fail "gh CLI 未安装"
command -v openssl >/dev/null || fail "openssl 未安装"

if ! gh auth status >/dev/null 2>&1; then
    fail "gh CLI 未认证。请先以 hrygo 身份登录: gh auth login"
fi

ACCOUNT=$(gh auth status 2>&1 | grep "Logged in" | grep -o 'account [^ ]*' | cut -d' ' -f2)
info "当前 gh 身份: ${ACCOUNT}"

# 检查仓库权限
PERMS=$(gh api "repos/${REPO}" --jq '.permissions' 2>/dev/null || echo "{}")
HAS_ADMIN=$(echo "$PERMS" | grep -o '"admin":true' || true)

if [ -z "$HAS_ADMIN" ]; then
    fail "当前账号 ${ACCOUNT} 对 ${REPO} 无 admin 权限。请以 hrygo 身份登录: gh auth login"
fi

ok "前置检查通过（${ACCOUNT} 对 ${REPO} 有 admin 权限）"

# ─── 检查已有 Webhook ─────────────────────────────────────────────────────────
info "检查已有 Webhook..."

EXISTING=$(gh api "repos/${REPO}/hooks" --jq ".[] | select(.config.url | test(\"${PUBLIC_IP}\")) | .id" 2>/dev/null || echo "")

if [ -n "$EXISTING" ]; then
    warn "已存在指向 ${PUBLIC_IP} 的 Webhook (ID: ${EXISTING})"
    read -p "是否更新? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        info "跳过。如需删除: gh api repos/${REPO}/hooks/${EXISTING} --method DELETE"
        exit 0
    fi
    # 删除旧的，重新创建
    gh api "repos/${REPO}/hooks/${EXISTING}" --method DELETE >/dev/null 2>&1
    ok "已删除旧 Webhook"
fi

# ─── 注册 Webhook ────────────────────────────────────────────────────────────
info "注册 GitHub Webhook..."

RESULT=$(gh api "repos/${REPO}/hooks" \
    --method POST \
    --field name=web \
    --field active=true \
    --field 'events[]=pull_request' \
    --field 'events[]=check_suite' \
    --field 'events[]=check_run' \
    --field "config[url]=https://${PUBLIC_IP}${WEBHOOK_PATH}" \
    --field 'config[content_type]=json' \
    --field "config[secret]=${SECRET}" \
    --field 'config[insecure_ssl]=1' \
    --jq '{id, url: .config.url, active, created_at}' 2>&1) || \
    fail "Webhook 注册失败: ${RESULT}"

ok "Webhook 注册成功 ✓"
echo "$RESULT" | jq .

# ─── 验证 Ping ───────────────────────────────────────────────────────────────
info "验证 Ping 事件..."

sleep 3
HOOK_ID=$(echo "$RESULT" | jq -r '.id')
PING_CODE=$(gh api "repos/${REPO}/hooks/${HOOK_ID}" --jq '.last_response.code' 2>/dev/null || echo "N/A")

if [ "$PING_CODE" = "200" ] || [ "$PING_CODE" = "204" ]; then
    ok "Ping 事件成功投递 (${PING_CODE}) ✓"
else
    warn "Ping 待确认 (code: ${PING_CODE})"
    info "手动检查: https://github.com/${REPO}/settings/hooks"
fi

# ─── 完成 ────────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
ok "GitHub Webhook 配置完成！"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "  Webhook URL:    https://${PUBLIC_IP}${WEBHOOK_PATH}"
echo "  Secret:         ${SECRET:0:8}..."
echo "  Events:         pull_request, check_suite, check_run"
echo "  SSL Verify:     Off（自签名证书）"
echo ""
echo "管理:"
echo "  查看:  https://github.com/${REPO}/settings/hooks"
echo "  删除:  gh api repos/${REPO}/hooks/${HOOK_ID} --method DELETE"
echo "  投递:  https://github.com/${REPO}/settings/hooks/${HOOK_ID}/deliveries"
echo ""
echo "⚠️  请将 Secret 同步到服务器:"
echo "  在服务器上运行:"
echo "  sed -i 's|^HOTPLEX_WEBHOOK_SECRET=.*|HOTPLEX_WEBHOOK_SECRET=${SECRET}|' ~/.hotplex/.env"
echo "  或直接编辑: vim ~/.hotplex/.env"
echo ""
