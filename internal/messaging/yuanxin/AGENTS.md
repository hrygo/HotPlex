# Yuanxin (元芯) Adapter

## OVERVIEW
元芯平台适配器。传输层用 **Apache Pulsar** 消息队列（非 HTTP/WebSocket），消费请求 topic、生产响应 topic。嵌入泛型 `BaseAdapter[*YuanxinConn]`，非流式（缓冲 delta，Done 时整条回发）。

## STRUCTURE
```
yuanxin/
  adapter.go          # Adapter + YuanxinConn + Pulsar 消费/生产 + WriteCtx 分发（690 行）
  adapter_test.go     # 测试
```

## WHERE TO LOOK
| 任务 | 位置 | 说明 |
|------|------|------|
| 注册 | `adapter.go:19` | `init()` → `messaging.Register(PlatformYuanxin, ...)` |
| 配置 | `adapter.go:57` | `ConfigureWith`: app_id 必填，pulsar_url/tenant/ns/producer_topic 有默认 |
| Pulsar 连接 | `adapter.go:211` | `connect`: NewClient → Subscribe consumer → CreateProducer |
| 消费 topic | `adapter.go:237` | `persistent://<tenant>/<ns>/chatbot-<appID>-request`，Shared 订阅 |
| 生产 topic | `adapter.go:248` | 默认 `global-open-claw-response-topic`，含 `://` 则原样用 |
| 重连循环 | `adapter.go:116` | `runConsumer`: 指数退避（默认 2s/60s），3 次 receive 重试 |
| 消息处理 | `adapter.go:329` | `handleMessage`: 解析 `YuanxinMessage` → Sanitize → Dedup → Gate → Bridge |
| 消息格式 | `adapter.go:324` | `YuanxinMessage{Metadata map, Msg string}`，Metadata 承载路由 |
| 用户标识 | `adapter.go:352` | `metadata.replyUserCodes` 作 userID，缺失回退 msg ID |
| 发送响应 | `adapter.go:495` | `SendResponse`: producer.Send，Metadata 回带 conn 缓存 |
| Cron 投递 | `adapter.go:405` | `SendCronResult`: 重组 Metadata（botId/messageId/sysId/platform="yx"） |
| WriteCtx 分发 | `adapter.go:573` | delta 累积 → Done 整发；Error 发错误文本；交互降级纯文本 |
| 连接实现 | `adapter.go:531` | `YuanxinConn`: channelID/threadKey/metadata/textBuilder |

## KEY PATTERNS

**Pulsar 双 topic 模型**
- 收：consumer 订阅 `chatbot-<appID>-request`，`SubscriptionName=hotplex-gateway-<appID>`
- 发：producer 写 `<producerTopic>`，单条消息完整投递（无流式分片）
- URL 协议限 `pulsar://` 或 `pulsar+ssl://`（`adapter.go:66` 校验）

**非流式缓冲（`YuanxinConn.WriteCtx`）**
- `MessageDelta` 累积进 `textBuilder`（1MB 上限 `maxTextBuilderSize`）
- 仅 `Done`/`Error` 触发 `SendResponse` 整条发送
- 与 Slack/Feishu 的分块流式不同：元芯只有"全文一次性回发"

**Metadata 透传**
- 请求 Metadata（`replyUserCodes`/`sysId`/`secret` 等）存入 conn
- 响应原样回带，平台侧据此路由到发起方
- Cron 投递时重组 Metadata，补 `messageId`/`botId`

**交互降级为纯文本**
- permission/question/elicitation 无卡片，渲染成中文提示串
- 例：`"权限请求：<tool>\n\n说明：<desc>\n\n选项：允许(allow)/拒绝(deny)"`（`adapter.go:649`）
- 用户回复走 Bridge.Handle 自然语言路径

**泛型 BaseAdapter（`adapter.go:30`）**
- `BaseAdapter[*YuanxinConn]` 是较 Slack/Feishu 更新的泛型基座
- `InitConnPool` 工厂按 `channelID#threadKey` 建 conn

## ANTI-PATTERNS
- ❌ 直接连 Pulsar 不走 `connect` —— 会绕过双检锁与旧 client 清理（`adapter.go:264`）
- ❌ 响应不带 Metadata —— 平台侧无法路由，消息会丢
- ❌ 假设流式 —— 元芯只收完整文本，不要在 WriteCtx 里分片发送
- ❌ `consumer.Receive` 失败不退出 —— 超过 3 次重试必须退出重连，否则卡死
