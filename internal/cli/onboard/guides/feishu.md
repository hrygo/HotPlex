
  ── Feishu Setup Guide ──────────────────────────

  1. Create App: https://open.feishu.cn/app → "创建企业自建应用"
  2. Add Permissions (权限管理 → 批量导入):
     im:message, im:message:send_as_bot, im:message.group_msg,
     im:message.group_msg:readonly, im:message.p2p_msg,
     im:message.p2p_msg:readonly, im:message.reactions:write_only,
     im:resource, im:resource:download, im:chat, im:chat:readonly,
     bot:info
  3. Enable Event Subscription (事件订阅 → WebSocket 模式):
     im.message.receive_v1 (必须)
     chat_access.event.bot_p2p_chat_entered_v1 (必须)
     im.message.read_v1, im.message.reaction.created_v1,
     im.message.reaction.deleted_v1 (推荐)
  4. Get Credentials: 凭证与基础信息 → App ID + App Secret
  5. Optional: speech:stt (云端语音转文字，仅 STT_PROVIDER=feishu 时)

  Docs: https://open.feishu.cn/document/home
