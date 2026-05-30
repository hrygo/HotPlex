#!/usr/bin/env bash
# verify-webhook.sh — 从公网验证 Webhook 端点端到端可达性
#
# 用法: bash scripts/verify-webhook.sh [PUBLIC_IP] [SECRET]
#
# 默认值:
#   PUBLIC_IP = 43.106.12.60
#   SECRET    = (使用服务端生成的 secret)
set -uo pipefail

PUBLIC_IP="${1:-43.106.12.60}"
SECRET="${2:-}"
[ -z "$SECRET" ] && { echo "Error: SECRET required (arg 2). Usage: $0 [PUBLIC_IP] [SECRET]"; exit 1; }
WEBHOOK_PATH="/api/webhook/github"
BASE_URL="https://${PUBLIC_IP}"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'
PASS=0
FAIL=0

pass() { echo -e "${GREEN}✓ PASS${NC} $*"; ((PASS++)); }
fail() { echo -e "${RED}✗ FAIL${NC} $*"; ((FAIL++)); }
info() { echo -e "\033[0;34m[TEST]${NC} $*"; }

echo "═══════════════════════════════════════════════════════════════"
echo "  Webhook 端到端验证"
echo "  目标: ${BASE_URL}"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# ─── Test 1: 路由隔离 — 根路径应返回 404 ──────────────────────
info "1/5: 路由隔离 — 根路径应返回 404"
HTTP=$(curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 10 "${BASE_URL}/" 2>/dev/null)
if [ "$HTTP" = "404" ]; then
	pass "根路径 → 404"
elif [ "$HTTP" = "000" ]; then
	fail "根路径 → 连接失败（端口未开放或 Caddy 未监听）"
else
	fail "根路径 → ${HTTP}（期望 404）"
fi

# ─── Test 2: 路由隔离 — /ws 应返回 404 ────────────────────────
info "2/5: 路由隔离 — /ws 应返回 404"
HTTP=$(curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 10 "${BASE_URL}/ws" 2>/dev/null)
if [ "$HTTP" = "404" ]; then
	pass "/ws 路径 → 404"
elif [ "$HTTP" = "000" ]; then
	fail "/ws 路径 → 连接失败"
else
	fail "/ws 路径 → ${HTTP}（期望 404）"
fi

# ─── Test 3: Webhook 无签名 → 403 Forbidden ───────────────────
info "3/5: Webhook 无签名 → 应被拒绝 (403)"
HTTP=$(curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 10 \
	-X POST \
	-H "X-GitHub-Event: ping" \
	-d '{}' \
	"${BASE_URL}${WEBHOOK_PATH}" 2>/dev/null)
if [ "$HTTP" = "403" ]; then
	pass "无签名请求 → 403 Forbidden"
elif [ "$HTTP" = "000" ]; then
	fail "无签名请求 → 连接失败"
else
	fail "无签名请求 → ${HTTP}（期望 403）"
fi

# ─── Test 4: Webhook 伪造签名 → 403 Forbidden ─────────────────
info "4/5: Webhook 伪造签名 → 应被拒绝 (403)"
HTTP=$(curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 10 \
	-X POST \
	-H "X-Hub-Signature-256: sha256=deadbeef" \
	-H "X-GitHub-Event: ping" \
	-d '{}' \
	"${BASE_URL}${WEBHOOK_PATH}" 2>/dev/null)
if [ "$HTTP" = "403" ]; then
	pass "伪造签名 → 403 Forbidden"
elif [ "$HTTP" = "000" ]; then
	fail "伪造签名 → 连接失败"
else
	fail "伪造签名 → ${HTTP}（期望 403）"
fi

# ─── Test 5: Webhook 有效签名 + ping → 应被接受 ───────────────
info "5/5: Webhook 有效签名 + ping 事件 → 应被接受"
PAYLOAD='{"zen":"Keep it logically awesome.","hook_id":123456,"hook":"web","repository":{"full_name":"hrygo/hotplex"}}'
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')
FULL_SIG="sha256=${SIGNATURE}"

HTTP=$(curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 10 \
	-X POST \
	-H "Content-Type: application/json" \
	-H "X-Hub-Signature-256: ${FULL_SIG}" \
	-H "X-GitHub-Event: ping" \
	-d "$PAYLOAD" \
	"${BASE_URL}${WEBHOOK_PATH}" 2>/dev/null)
if [ "$HTTP" = "200" ] || [ "$HTTP" = "204" ]; then
	pass "有效签名 ping → ${HTTP}（已接受）"
elif [ "$HTTP" = "403" ]; then
	fail "有效签名 ping → 403（签名验证失败，检查 SECRET 是否匹配）"
elif [ "$HTTP" = "000" ]; then
	fail "有效签名 ping → 连接失败"
else
	# 可能返回其他状态码（如 202、500 等），部分通过
	fail "有效签名 ping → ${HTTP}（期望 200/204，签名可能未通过或有内部错误）"
fi

# ─── 结果汇总 ──────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo -e "  结果: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC} / $((PASS + FAIL)) total"
echo "═══════════════════════════════════════════════════════════════"
echo ""

if [ "$FAIL" -eq 0 ]; then
	echo -e "${GREEN}🎉 Webhook 端点验证全部通过！可以注册 GitHub Webhook。${NC}"
	echo ""
	echo "注册命令:"
	echo "  bash scripts/setup-github-webhook.sh ${SECRET:0:8}..."
else
	echo -e "${RED}⚠️  有 ${FAIL} 项验证失败，请检查上面的输出。${NC}"
fi

exit "$FAIL"
