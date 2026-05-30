#!/usr/bin/env bash
# setup-github-webhook.sh — 在 hrygo/hotplex 仓库注册 GitHub Webhook
#
# 用法: bash scripts/setup-github-webhook.sh <webhook-secret>
#
# 参数:
#   <webhook-secret>  必填。由 setup-webhook-pr-review.sh 生成的 Secret。
#
# 前置条件:
#   - gh CLI 已以 hrygo（或对 hrygo/hotplex 有 admin 权限）身份登录
#   - 服务器侧已执行: sudo bash scripts/setup-webhook-pr-review.sh
#
# 职责（仅此一项）:
#   在 GitHub 注册 Repository Webhook，验证 Ping 投递。
#   不生成 Secret、不碰服务器文件、不操作 Caddy。
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

# ─── 参数校验 ─────────────────────────────────────────────────────────────────
SECRET="${1:-}"

if [ -z "$SECRET" ]; then
    fail "用法: bash $0 <webhook-secret>
  Secret 由服务器侧脚本生成，参见 setup-webhook-pr-review.sh 输出"
fi

# ─── 前置检查 ─────────────────────────────────────────────────────────────────
info "前置检查..."

command -v gh >/dev/null || fail "gh CLI 未安装"

if ! gh auth status >/dev/null 2>&1; then
    fail "gh CLI 未认证。请以 hrygo 身份登录: gh auth login"
fi

ACCOUNT=$(gh auth status 2>&1 | grep -o 'account [^ ]*' | cut -d' ' -f2)
info "当前 gh 身份: ${ACCOUNT}"

# 检查仓库 admin 权限
PERMS=$(gh api "repos/${REPO}" --jq '.permissions' 2>/dev/null || echo "{}")
HAS_ADMIN=$(echo "$PERMS" | grep -o '"admin":true' || true)

if [ -z "$HAS_ADMIN" ]; then
    fail "当前账号 ${ACCOUNT} 对 ${REPO} 无 admin 权限。
  请以 hrygo 身份登录: gh auth login
  或切换账号: gh auth switch"
fi

ok "前置检查通过（${ACCOUNT} 对 ${REPO} 有 admin 权限）"

# ─── 幂等检查 ─────────────────────────────────────────────────────────────────
info "检查已有 Webhook..."

EXISTING_ID=$(gh api "repos/${REPO}/hooks" \
    --jq ".[] | select(.config.url | test(\"${PUBLIC_IP}\")) | .id" 2>/dev/null || echo "")

if [ -n "$EXISTING_ID" ]; then
    warn "已存在指向 ${PUBLIC_IP} 的 Webhook (ID: ${EXISTING_ID})"
    read -p "删除并重新创建? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        info "跳过。管理: https://github.com/${REPO}/settings/hooks"
        exit 0
    fi
    gh api "repos/${REPO}/hooks/${EXISTING_ID}" --method DELETE >/dev/null 2>&1
    ok "已删除旧 Webhook (ID: ${EXISTING_ID})"
fi

# ─── 注册 Webhook ────────────────────────────────────────────────────────────
info "注册 GitHub Webhook..."

HOOK_ID=$(gh api "repos/${REPO}/hooks" \
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
    --jq '.id' 2>&1) || \
    fail "Webhook 注册失败: ${HOOK_ID}"

ok "Webhook 注册成功 (ID: ${HOOK_ID}) ✓"

# ─── 验证 Ping ───────────────────────────────────────────────────────────────
info "验证 Ping 事件..."

sleep 3
PING_CODE=$(gh api "repos/${REPO}/hooks/${HOOK_ID}" --jq '.last_response.code' 2>/dev/null || echo "N/A")

if [ "$PING_CODE" = "200" ] || [ "$PING_CODE" = "204" ]; then
    ok "Ping 成功投递 (${PING_CODE}) ✓"
else
    warn "Ping 待确认 (HTTP ${PING_CODE})"
    info "手动检查: https://github.com/${REPO}/settings/hooks/${HOOK_ID}/deliveries"
fi

# ─── 完成 ────────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
ok "GitHub Webhook 注册完成！"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "  Webhook URL:   https://${PUBLIC_IP}${WEBHOOK_PATH}"
echo "  Hook ID:       ${HOOK_ID}"
echo "  Events:        pull_request, check_suite, check_run"
echo "  SSL Verify:    Off（自签名证书）"
echo ""
echo "管理:"
echo "  设置:   https://github.com/${REPO}/settings/hooks"
echo "  投递:   https://github.com/${REPO}/settings/hooks/${HOOK_ID}/deliveries"
echo "  删除:   gh api repos/${REPO}/hooks/${HOOK_ID} --method DELETE"
echo ""
