---
title: Streaming Delta Pipeline — Worker 到 WebChat 的流式增量全链路
---

# Streaming Delta Pipeline — Worker 到 WebChat 的流式增量全链路

> 配套文档：
> [WebSocket-Full-Duplex-Flow.md](./WebSocket-Full-Duplex-Flow.md)（全双工通信总览）
> · [Worker-Gateway-Design.md](./Worker-Gateway-Design.md)（Worker/Bridge/Hub 设计）
> · [AEP-v1-Protocol.md](./AEP-v1-Protocol.md)（AEP v1 协议规范）
> · [AEP-v1-Appendix.md](./AEP-v1-Appendix.md)（事件类型清单）

本文详细描绘一个 `message.delta` 事件从 **Worker 子进程 stdout 字节** 到 **WebChat 浏览器像素** 的完整生命周期，覆盖四种 Worker 适配器的差异、Gateway 内部的 12 步变换、三层背压模型，以及 WebChat 的同步提交 + 无条件 reconcile 机制。

---

## 目录

- [Streaming Delta Pipeline — Worker 到 WebChat 的流式增量全链路](#streaming-delta-pipeline--worker-到-webchat-的流式增量全链路)
  - [目录](#目录)
  - [1. 鸟瞰：四段九跳](#1-鸟瞰四段九跳)
  - [2. Worker 生产层：四种适配器的 delta 转换](#2-worker-生产层四种适配器的-delta-转换)
    - [2.1 适配器差异对照](#21-适配器差异对照)
    - [2.2 claudecode（stdio NDJSON + 后缀差分）](#22-claudecodestdio-ndjson--后缀差分)
    - [2.3 codexcli（app-server 单例 + drift 修正）](#23-codexcliapp-server-单例--drift-修正)
    - [2.4 opencodeserver（HTTP+SSE 纯增量）](#24-opencodeserverhttpsse-纯增量)
    - [2.5 acp（JSON-RPC + 合成 MessageStart/End）](#25-acpjson-rpc--合成-messagestartend)
  - [3. Bridge 转发层：单 goroutine 串行化 + 12 步变换](#3-bridge-转发层单-goroutine-串行化--12-步变换)
  - [4. Hub 广播层：第一层背压](#4-hub-广播层第一层背压)
    - [4.1 droppable vs guaranteed 投递](#41-droppable-vs-guaranteed-投递)
    - [4.2 多订阅者分发语义](#42-多订阅者分发语义)
  - [5. Conn → Browser：第二/三层背压的不对称设计](#5-conn--browser第二三层背压的不对称设计)
  - [6. WebChat 消费与渲染层](#6-webchat-消费与渲染层)
  - [7. 背压与 Reconcile 完整时序](#7-背压与-reconcile-完整时序)
    - [7.1 无条件 Reconcile](#71-无条件-reconcile)
    - [7.2 unmount guard](#72-unmount-guard)
  - [8. 缓冲容量与可丢弃事件分类](#8-缓冲容量与可丢弃事件分类)
  - [9. 持久化与平台 Coalescing 旁路](#9-持久化与平台-coalescing-旁路)
    - [9.1 eventstore delta 聚合](#91-eventstore-delta-聚合)
    - [9.2 pcEntry 平台 delta coalescing](#92-pcentry-平台-delta-coalescing)
  - [10. 关键源码索引](#10-关键源码索引)
    - [协议层](#协议层)
    - [Worker 层](#worker-层)
    - [Bridge 层](#bridge-层)
    - [Hub 层](#hub-层)
    - [Conn 层](#conn-层)
    - [持久化层](#持久化层)
    - [平台层](#平台层)
    - [WebChat 层](#webchat-层)
  - [11. 一句话浓缩](#11-一句话浓缩)
  - [12. 设计准则](#12-设计准则)
    - [12.1 关键数据路径不依赖浏览器节流 API](#121-关键数据路径不依赖浏览器节流-api)
    - [12.2 不为 cosmetic 数据引入跨层标记协议](#122-不为-cosmetic-数据引入跨层标记协议)
    - [12.3 不用阻塞串行化保护可丢弃事件](#123-不用阻塞串行化保护可丢弃事件)

---

## 1. 鸟瞰：四段九跳

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         一个 message.delta 的生命                            │
└─────────────────────────────────────────────────────────────────────────────┘

   ╔═════════════╗      ╔═══════════════╗      ╔═══════════════╗      ╔══════════════╗
   ║ ① Worker    ║ ──► ║ ② Base.Conn   ║ ──► ║ ③ Bridge      ║ ──► ║ ④ Hub        ║
   ║   Adapter   ║      ║   recvCh      ║      ║   forwardEvt  ║      ║   broadcast  ║
   ║ (4种实现)   ║      ║   cap=256     ║      ║   单goroutine ║      ║   cap=256    ║
   ╚═════════════╝      ╚═══════════════╝      ╚═══════════════╝      ╚══════════════╝
                                                                          │
                                                                          ▼
        ╔═══════════════╗      ╔═══════════════╗      ╔═══════════════════════╗
        ║ ⑧ React DOM  ║ ◄──  ║ ⑦ Markdown    ║ ◄──  ║ ⑤ Conn.writeCh       ║
        ║   像素渲染    ║      ║   re-render   ║      ║   cap=256 → WritePump ║
        ║              ║      ║   ≤60fps(RAF) ║      ║   → WS frame         ║
        ╚═══════════════╝      ╚═══════════════╝      ╚═══════════════════════╝
                                                          │
                                                          ▼ (NDJSON TextMessage)
                                                  ╔═══════════════════════════╗
                                                  ║ ⑥ Browser WS onMessage    ║
                                                  ║   deserializeEnvelope     ║
                                                  ║   EventEmitter 分发       ║
                                                  ║   → RAF batch → setState  ║
                                                  ╚═══════════════════════════╝
```

九个关键节点：

1. **Worker 解析**（4 种协议 → 统一 AEP）
2. **recvCh**（cap=256 非阻塞）
3. **Bridge forwardEvents**（per-session 单 goroutine）
4. **Hub.SendToSession**（seq 盖戳 + 背压分流）
5. **Conn.writeCh**（cap=256，per-conn 背压）
6. **Browser WS**（NDJSON 解码 + EventEmitter）
7. **RAF + Markdown**（≤60fps 批量）
8. **DOM 像素**（CSS 闪烁光标）

---

## 2. Worker 生产层：四种适配器的 delta 转换

四种适配器都最终产出 `events.MessageDelta` Kind（`pkg/events/events.go:23`）+ `MessageDeltaData{MessageID, Content}`（`events.go:204-207`），但**来源协议、是否差分、MessageID 来源**差异巨大。

### 2.1 适配器差异对照

| 适配器             | 底层进程协议                                 | delta 源                                                | 差分方式                                              | MessageID                                   |
| ------------------ | -------------------------------------------- | ------------------------------------------------------- | ----------------------------------------------------- | ------------------------------------------- |
| **claudecode**     | stdio NDJSON (`--output-format stream-json`) | `stream_event`（真增量）+ `assistant`（全量快照）       | `getDeltaText` 简单 rune 后缀，无 drift 检测          | `streamEvt.Message.ID` 或 `"assistant_msg"` |
| **codexcli**       | stdio JSON-RPC 2.0（app-server 单例）        | `item/agentMessage/delta`（真）+ `item/updated`（全量） | `getDeltaText` + drift 检测/纠正（`commonPrefixLen`） | `messageTracker` 生成的 `evt_xxx`           |
| **opencodeserver** | HTTP + SSE（`opencode serve` 单例）          | `message.part.delta`（纯增量）                          | 无差分（直传）                                        | **空字符串**（converter 不设）              |
| **acp**            | stdio JSON-RPC 2.0（per-session 进程）       | `agent_message_chunk`（纯增量）                         | 无差分（直传）                                        | `"msg_" + sessionID`                        |

### 2.2 claudecode（stdio NDJSON + 后缀差分）

```
 claude --print --verbose
   --output-format stream-json        ┌──────────────────────────────┐
   --input-format stream-json         │ stdout 一行 = 一个 SDKMessage │
   --session-id <id>                  │ (proc/manager.go:433 ReadLine│
        │                             │  bufio.Scanner buf=10MB)     │
        ▼                             └──────────────┬───────────────┘
 ┌─────────────────────┐                             │ NDJSON line
 │ readOutput goroutine│ (worker.go:698-836)         ▼
 │ for { line=read() } │                  ┌─────────────────────────┐
 │ json.Unmarshal SDK  │ ──────────────►  │ parser.ParseMessage     │
 └─────────────────────┘                  │ (parser.go:113)         │
                                          └────────────┬────────────┘
                                                       │ WorkerEvent
                          ┌────────────────────────────┴───────────────────┐
                          │  stream_event 路径                              │  assistant 路径
                          ▼                                                ▼
              parseStreamEvent                              parseAssistant
              (parser.go:138)                                (parser.go:190)
              StreamPayload{                                 StreamPayload{
                IsDelta:true,  ← 真增量                       IsDelta:false, ← 全量
                Content, Type                                 Content, Type
              }                                              }
                          │                                                │
                          └────────────────┬───────────────────────────────┘
                                           ▼
                           mapper.mapStream (mapper.go:126-163)
                                           │
                  ┌────────────────────────┴────────────────────────┐
                  │  if IsDelta:                                     │
                  │     recordSentDelta(...)  // 累计已发             │
                  │     content = p.Content                          │
                  │  else:                                           │
                  │     content = getDeltaText(...) // 后缀差分       │
                  │     currRunes[lastLen:]                          │
                  │     (mapper.go:348-383)                          │
                  └────────────────────────┬────────────────────────┘
                                           ▼
                  events.NewEnvelope(
                    id, sessionID, seqGen.next(),
                    events.MessageDelta,
                    MessageDeltaData{MessageID, Content})
                                           │
                                           ▼
                  worker.trySend(env) (worker.go:915-931)
                  → base.Conn.TrySend (非阻塞)
                  → recvCh <- env    [满了 warn 丢弃]
```

**差分逻辑**（`mapper.go:348-383`）：按 rune 切片做"已发长度后缀"差分，`currRunes[lastLen:]` 为新 delta，并累计 `sentTexts[itemID] += delta`。**注意：claudecode 版本是简单后缀差分，没有 drift 检测/修正**（与 codexcli 不同）。

### 2.3 codexcli（app-server 单例 + drift 修正）

```
 codex app-server 进程(全局单例)
   │  stdio JSON-RPC 2.0
   ▼
 ┌──────────────────────────────────┐    每 thread 一个
 │ manager.readNotifications        │    subscriber channel
 │ (manager.go:506)                 │ ──►  chan *Envelope cap=256
 │ bufio.Scanner buf=10MB           │
 └──────────────┬───────────────────┘
                │ 一行 = 一个 JSON-RPC frame
                ▼
      dispatchFrame (manager.go:552)
                │ 无 ID+有 Method
                ▼
      dispatchNotification (manager.go:935)
                │
                │ getOrCreateConverter(threadID)
                ▼
      conv.MapNotification(method, params)  (mapper.go:64)
                │
   ┌────────────┴────────────┬───────────────────────┐
   ▼                         ▼                       ▼
 item/agentMessage/      item/updated             item/started
 delta (真增量)           (全量快照)               /completed
   │                         │                       │
   ▼                         ▼                       ▼
 mapNotifDelta            mapItemUpdated          messageTracker
 (mapper.go:436)          (mapper.go:362)          → MessageStart/End
   │                         │
   │ recordSentDelta         │ getDeltaText(item.ID, item.Text)
   │ content=Delta           │  (mapper.go:858-920)
   │                         │
   │                  ┌──────┴───────────────────────────┐
   │                  │ 1. commonPrefixLen(sent,snap)     │
   │                  │ 2. 若 prefix < len(sent) → drift!│
   │                  │    发 snap[prefix:] 纠正           │
   │                  │    sent=snap 重置基线              │
   │                  │    driftCount++                   │
   │                  │ 3. 否则 sent[len(sent):] 后缀差分  │
   │                  └──────┬───────────────────────────┘
   │                         │
   └────────────┬────────────┘
                ▼
   newEnvelope(MessageDelta, {MessageID, Content})
                │
                ▼
   manager.sendEnvelope (manager.go:996)
   ┌──────────────────────────────────────────┐
   │ if MessageDelta: 非阻塞丢弃(满则 warn)   │
   │ else:         5s criticalEventSendTimeout │
   └──────────────────────────────────────────┘
```

**drift 检测是 codex 独有**（`mapper.go:858-920`）：不仅做后缀差分，还做前缀一致性校验。drift 时发纠正 delta 并重置基线，`DriftCount` 累计上报到 `DoneData.Stats["delta_drifts"]`。

### 2.4 opencodeserver（HTTP+SSE 纯增量）

```
 opencode serve --port <ephemeral>
   │  HTTP server 单例
   ▼
 ┌──────────────────────────────────┐
 │ readGlobalSSE goroutine          │
 │ (singleton.go:496-599)           │
 │ 长连 /global/event               │
 │ text/event-stream                │ ──► 每 session
 │ 逐行 ReadString('\n')            │      chan *Envelope cap=256
 │ 提取 "data: " 前缀               │
 └──────────────┬───────────────────┘
                ▼
      dispatchOCSEvent (singleton.go:603)
                │
                ▼
      converter.Convert(sessionID, type, props)
                │
                ▼
      handlePartDelta (converter.go:175-213)
      ┌────────────────────────────────────────────┐
      │ evt.Field:                                 │
      │   "reasoning" → events.Reasoning           │
      │   default/"text":                          │
      │     if reasoningActive → Reasoning         │
      │     else → events.MessageDelta{            │
      │              Content: evt.Delta,           │
      │              MessageID: ""  ← 空!          │
      │            }                               │
      └────────────────────────────────────────────┘
                │
                ▼
      worker.forwardBusEvents (worker.go:762-818)
      ┌────────────────────────────────────────────┐
      │ isDroppable(MessageDelta|Raw):             │
      │   非阻塞丢弃                               │
      │ else: trySendEnvelope 5s 阻塞              │
      └────────────────────────────────────────────┘
```

**特点**：delta 是纯增量，**不需要 `getDeltaText` 差分**。`reasoningActive` 状态机（`session.next.reasoning.started/ended`，`converter.go:91-100`）切换；reasoning 期间的 text delta 也会被路由为 `Reasoning`。

### 2.5 acp（JSON-RPC + 合成 MessageStart/End）

```
 hermes acp 子进程
   │  stdio JSON-RPC 2.0
   ▼
 ┌──────────────────────────────────┐
 │ ACPClient.readLoop               │
 │ (client.go:221-271)              │
 │ bufio.Scanner (acp/codec.go)     │ ──► NotificationCh cap=256
 │                                  │      (满则 warn 丢弃)
 └──────────────┬───────────────────┘
                ▼
      worker.readLoop (worker.go:1028)
                │
                ▼
      mapper.MapNotification (mapper.go:84)
      ┌────────────────────────────────────────────┐
      │ session/update → 按 type 分发              │
      │                                            │
      │ agent_message_chunk:                       │
      │   if !msgActive.Swap(true):                │
      │     ← 首 chunk 合成 MessageStart           │
      │   ← MessageDelta{msgID:"msg_"+sid,Content} │
      │                                            │
      │ agent_thought_chunk:                       │
      │   ← Reasoning                              │
      └────────────────────────────────────────────┘
                │
                ▼
      conn.TrySend (acp/conn.go:59-80)
      ┌────────────────────────────────────────────┐
      │ isDroppable(MessageDelta|Raw):             │
      │   trySendNonBlocking 静默丢                │
      │ else: safeSend 5s 阻塞 + recover           │
      │       (防 send-on-closed panic)            │
      └────────────────────────────────────────────┘
```

**特点**：`msgActive atomic.Bool` 状态机——首 chunk 触发合成 `MessageStart`；`MapPromptResponse`/`MapPromptError` 调 `closeMessageStream`（`mapper.go:171-180`）发 `MessageEnd`。

---

## 3. Bridge 转发层：单 goroutine 串行化 + 12 步变换

**核心架构特性**：每个 session **只有一个 forwardEvents goroutine** 独占 `forwardContext`（`bridge_forward.go:30-50`），无锁。多 WS 订阅者由 Hub 内部 map 扇出，**不**在此层并行。

```
                      recvCh <-chan *events.Envelope (cap=256)
                                     │
                                     ▼
        ┌────────────────────────────────────────────────────────┐
        │ forwardEvents goroutine (per session, 独占 fc)          │
        │ (bridge_forward.go:65-183)                             │
        │                                                        │
        │  resetGen 捕获 ──────────────────────────────┐          │
        │  (旧 goroutine 在 recvCh 关闭后 mismatch 退出) │          │
        │  turnTimer 起闹 ─────────────────────────┐   │          │
        │                                          ▼   │          │
        │  ┌──── for env := range recvCh ◄── ① 入口 ────┘          │
        │  │                                                       │
        │  └─► processForwardedEvent(env, w, opts, fc)             │
        │              (bridge_forward.go:186)                     │
        │       │                                                  │
        │       ▼                                                  │
        │  ┌──────────────────────────────────────────────────┐   │
        │  │ ❶ SetLastIO(env≠Done)            :196-200        │   │
        │  │    刷新 worker 活跃时间防 zombie GC              │   │
        │  │                                                  │   │
        │  │ ❷ internal_reset 拦截(OCS/ACP)   :203-206       │   │
        │  │    return，不转发                                │   │
        │  │                                                  │   │
        │  │ ❸ Error 扣留                     :209-222        │   │
        │  │    if retryCtrl: fc.pendingError=Clone; return   │   │
        │  │    (retry 决策延到 Done，不污染 delta 流)        │   │
        │  │                                                  │   │
        │  │ ❹ firstEvent 安全网              :224-232        │   │
        │  │  ❺ state(running)→doneReceived=false :237       │   │
        │  │  ❻ turnTimer 检查丢弃            :241-246        │   │
        │  │                                                  │   │
        │  │ ❼ events.Clone(env) 深拷贝        :248          │   │
        │  │    消除 Hub 编码 vs capture 编码 data race       │   │
        │  │                                                  │   │
        │  │ ❽ extractTurnContent              :252,316-328  │   │
        │  │    fc.turnText += content; deltaContent 返回    │   │
        │  │                                                  │   │
        │  │ ❾ accumulateStats                :255,367-408   │   │
        │  │                                                  │   │
        │  │ ❿ Done 处理                       :258-268       │   │
        │  │    doneReceived=true                              │   │
        │  │    (无 dropped 标记，见 §7)                      │   │
        │  │                                                  │   │
        │  │ ⓫ hub.SendToSession(ctx, env)    :270  ★转发     │   │
        │  │                                                  │   │
        │  │ ⓬ captureForwardedEvent           :274,492-501   │   │
        │  │    if deltaContent != "":                        │   │
        │  │      collector.CaptureDeltaString(sid,seq,txt)   │   │
        │  │    (先转发后持久化，异步不阻塞转发)              │   │
        │  │                                                  │   │
        │  │ flushPendingError :277                           │   │
        │  │ LLM retry 决策 :280-306 (Done 后异步 autoRetry) │   │
        │  └──────────────────────────────────────────────────┘   │
        └────────────────────────────────────────────────────────┘
```

**delta 在 gateway 内部的 12 个变换点**（按时间顺序）：

| #   | 变换               | 位置                                      | 影响                                      |
| --- | ------------------ | ----------------------------------------- | ----------------------------------------- |
| 1   | SetLastIO          | `bridge_forward.go:196`                   | 防 zombie GC                              |
| 2   | turnText 累积      | `:252`                                    | turns 表 + retry 判定                     |
| 3   | events.Clone       | `:248`                                    | 深拷贝 map 防 race                        |
| 4   | Seq 分配           | `hub.go:351`                              | per-session 单调                          |
| 5   | TraceID 注入       | `hub.go:336`                              | COW 写 Metadata                           |
| 6   | EncodeJSON         | `hub.go:553`                              | 一次性编码 N conn 共享                    |
| 7   | 背压丢弃           | `hub.go:trySendBroadcast` / `conn.go:817` | broadcast 或 writeCh 满则静默丢（仅计数） |
| 8   | init 缓冲          | `conn.go:869`                             | initPending                               |
| 9   | 持久化聚合         | `bridge_forward.go:494`                   | 多 delta → 单 message                     |
| 10  | pcEntry coalescing | `platform_writer.go:170`                  | 多 delta → 单 merged                      |
| 11  | reconcile 触发     | 前端 `handleDone`                         | done 时无条件触发 `reconcileTurnContent`  |
| 12  | WS 帧封装          | `conn.go:715`                             | TextMessage + NDJSON                      |

---

## 4. Hub 广播层：第一层背压

### 4.1 droppable vs guaranteed 投递

**关键设计**：droppable 事件在 Hub 层走 `trySendBroadcast`（**带 default 的非阻塞 send**），broadcast chan 满立即丢弃——避免单慢 session 阻塞 Hub.Run 造成跨 session 级联丢失。guaranteed 事件（state/done/error/...）走 `sendBroadcast`（阻塞至 ctx done，保证必达）。

```
   SendToSession(ctx, env)  (hub.go:324)
              │
              ▼
   ┌──────────────────────────────────────────────────────────┐
   │ ❶ Seq 盖戳                                  hub.go:351   │
   │   if env.Seq == 0: env.Seq = seqGen.Next(sid)            │
   │   (per-session sync.Map[sid]→*atomic.Int64)              │
   │                                                          │
   │ ❷ TraceID 注入 (COW Metadata)               hub.go:336   │
   │                                                          │
   │ ❸ 第一层：isDroppable 判定                  hub.go:368   │
   │   ┌────────────────────────────────────────────┐         │
   │   │ isDroppable = MessageDelta|Reasoning|Raw    │         │
   │   │   ↓ droppable                               │         │
   │   │ trySendBroadcast(select default 立即返回)   │         │
   │   │   ↓ 满则静默丢 + 计数                        │         │
   │   │ broadcast chan *EnvelopeWithConn cap=256    │         │
   │   └────────────────────────────────────────────┘         │
   │   非 droppable(state/done/error): sendBroadcast           │
   │   阻塞至 ctx done（保证必达）                             │
   └──────────────────────┬───────────────────────────────────┘
                          │
                          ▼
   ┌──────────────────────────────────────────────────────────┐
   │ Hub.Run 单 goroutine (hub.go:479,496-518)                │
   │   select:                                                │
   │     registerCh / unregisterCh / broadcast                │
   │                                                          │
   │   ← msg := <-broadcast                                   │
   │   → routeMessage(msg) (hub.go:522)                       │
   └──────────────────────┬───────────────────────────────────┘
                          │
                          ▼
   ┌──────────────────────────────────────────────────────────┐
   │ routeMessage (hub.go:522-579)                            │
   │                                                          │
   │ ❹ snapshotConns(sid) (RLock 取订阅者切片) hub.go:262     │
   │     sessions[sid] = map[SessionWriter]bool               │
   │                                                          │
   │ ❺ 无订阅者 → GatewayNoSubscribersDropped return :525    │
   │                                                          │
   │ ❻ LogHandler 回调 (可选)                                  │
   │                                                          │
   │ ❼ aep.EncodeJSON(msg.Env) 一次性编码      hub.go:553    │
   │   (N 个订阅者共享一次编码结果)                            │
   │                                                          │
   │ ❽ for each conn in snapshotConns:                        │
   │     if conn.PreferEnvelope():  // pcEntry (平台)         │
   │       conn.RouteWrite(ctx, env)                          │
   │       (保留 json:"-" 字段 + 内部 delta coalescing)        │
   │     else:  // *Conn (WS)                                 │
   │       conn.RouteWriteData(data, eventType)               │
   │       (传预编码字节，省 N-1 次编码)                       │
   │     失败 → conn.Close() + removeSession                  │
   └──────────────────────┬───────────────────────────────────┘
                          │
              ┌───────────┴────────────┐
              ▼                        ▼
        WS Conn 路径                pcEntry 平台路径
        (见 §5)                    (delta coalescing，§9)
```

### 4.2 多订阅者分发语义

```
   ┌────────────────────────────────────────────────────────┐
   │ 实际订阅模型 (hub.go:212-235 JoinSession)              │
   │                                                        │
   │ WebChat session: 按 session_id 去重，只保留最新 conn   │
   │   JoinSession: 移除旧 conn(不 Close 让其自然死亡)      │
   │   → 普通 webchat session 同一时刻只有 1 个 WS conn     │
   │                                                        │
   │ 多订阅者真实场景: WS conn + pcEntry(平台)并存          │
   │   → routeMessage 遍历，两者各走一条路径                │
   └────────────────────────────────────────────────────────┘
```

---

## 5. Conn → Browser：第二/三层背压的不对称设计

**关键设计**：webchat 的 `*Conn.writeDispatch` **不**沿用 Hub 层的 `isDroppable` 分类，而是采用 per-event-type 的差异化背压策略：

```
   conn.RouteWriteData(data, eventType) (conn.go:774)
              │
              ▼
   writeDispatch (conn.go:787-796)
              │
              │ if initDone==false: bufferOrReject (conn.go:869)
              │   存入 initPending [][]byte
              │   (init_ack 是首条消息保证)
              │
              ▼
   ┌──────────────────────────────────────────────────────────┐
   │ 第二层背压 (per-conn writeCh cap=256 conn.go:113)         │
   │                                                          │
   │  eventType == events.Raw ?                               │
   │   ↓ YES                                                  │
   │  trySendData (conn.go:817)  — 静默丢弃                   │
   │   select case writeCh<-data: OK                          │
   │          default:           ← writeCh 满!               │
   │            observability.GatewayDeltasDropped().Add()    │
   │            return nil  (Raw 是 fire-and-forget)          │
   │   ↓ NO (delta / reasoning / state / done / error / ...)  │
   │  sendData (conn.go:800)  — guaranteed，满则断连          │
   │   select case writeCh<-data: OK                          │
   │          default:  ← writeCh 满!                         │
   │            conn.Close()  ← 断开慢 webchat 客户端         │
   │                                                          │
   │  ★ webchat 哲学: 慢客户端断连 → 重连 → 从 event store   │
   │    replay 完整内容。静默丢 delta 会让 UI 永远缺一角。    │
   │    断连是更强的保护，但对可控的本地 WS 客户端是值得的。  │
   └──────────────────────┬───────────────────────────────────┘
                          │
              ┌───────────┴────────────┐
              ▼                        ▼
       webchat (*Conn)          平台 (pcEntry)
       delta/reasoning:         delta/reasoning:
       guaranteed (断连)         droppable + coalescing
                                 (远程平台不可控，断连代价高)
                          │
                          ▼
   ┌──────────────────────────────────────────────────────────┐
   │ WritePump goroutine (per-conn, conn.go:689-736)          │
   │   select:                                                │
   │     c.done → return                                      │
   │     c.hb.Stopped() (missed pong) → return                │
   │     data := <-c.writeCh:                                 │
   │       c.mu.Lock()                                        │
   │       c.conn.SetWriteDeadline(now + writeWait=10s)       │
   │       c.conn.WriteMessage(TextMessage, data)  ← WS 帧    │
   │       c.mu.Unlock()                                      │
   │     ticker.C (pingPeriod=54s):                           │
   │       WriteMessage(PingMessage, nil)                     │
   └──────────────────────┬───────────────────────────────────┘
                          │
                          │ NDJSON TextMessage over TCP/TLS
                          ▼
   ╔═══════════════════════════════════════════════════════════╗
   ║                   ⑥ WebChat Browser                      ║
   ╚═══════════════════════════════════════════════════════════╝
```

---

## 6. WebChat 消费与渲染层

WebChat 技术栈：自研 `BrowserHotPlexClient`（基于 `eventemitter3`）+ 纯 React `useState` + assistant-ui 的 `ExternalStoreAdapter` 模式（**无** Zustand/Redux/Context）+ `react-markdown` + `remark-gfm` + `highlight.js`。

```
   ╔═══════════════════════════════════════════════════════════════════╗
   ║  WS onMessage (browser-client.ts:237)                             ║
   ║   const line = event.data.trim()                                  ║
   ║   const env = deserializeEnvelope(line)  (envelope.ts:282)        ║
   ║     └─ 清洗 U+2028/U+2029 → JSON.parse                           ║
   ║   client._handleMessage(env)                                      ║
   ║     ├─ isInitAck? → resolve(init)                                 ║
   ║     └─ _routeEvent(env) (browser-client.ts:335)                   ║
   ║          switch env.event.type:                                   ║
   ║            case MessageDelta:                                     ║
   ║              emit('delta', data, env)  ← ★分发点 (browser:369)   ║
   ╚═══════════════════════════════════════════════════════════════════╝
                              │
                              ▼
   ╔═══════════════════════════════════════════════════════════════════╗
   ║  hotplex-runtime-adapter.ts (纯 useState，无 Redux/Zustand)       ║
   ║                                                                   ║
   ║  client.on('delta', handleDelta) (adapter:988)                    ║
   ║                                                                   ║
   ║  ┌──────────────────────────────────────────────────────────────┐ ║
   ║  │ 步骤1: handleDelta (adapter:504-508) — 同步提交               │ ║
   ║  │   appendDelta(data.content || "")                             │ ║
   ║  │                                                             │ ║
   ║  │ ★ 同步提交，不使用 requestAnimationFrame：React 18 自动批处理│ ║
   ║  │   已把同一 microtask 内的多次 setMessages 合并成一次         │ ║
   ║  │   re-render。RAF 等浏览器节流 API 会在后台 tab 暂停，导致    │ ║
   ║  │   delta 无限累积丢失，因此不能用于关键数据路径。            │ ║
   ║  └──────────────────────────────────────────────────────────────┘ ║
   ║                              │                                    ║
   ║                              ▼                                    ║
   ║  ┌──────────────────────────────────────────────────────────────┐ ║
   ║  │ 步骤2: appendDelta (adapter:435-459)                         │ ║
   ║  │   setMessages(prev => {                                      │ ║
   ║  │     last = prev[len-1]                                       │ ║
   ║  │     if last.role=="assistant" && last.status=="streaming":   │ ║
   ║  │       parts = appendTextDelta(last.parts, content)           │ ║
   ║  │       return [...prev.slice(0,-1), {...last, parts}]         │ ║
   ║  │     else:                                                    │ ║
   ║  │       // 无 streaming → 建 fallback (message.start 可能迟到)│ ║
   ║  │       streamingFallbackId = `assistant-${Date.now()}`        │ ║
   ║  │       return [...prev, {id:fallback, role:"assistant",       │ ║
   ║  │                       parts:[{type:"text",text:content}],    │ ║
   ║  │                       status:"streaming", createdAt:new Date}]│ ║
   ║  │   })                                                         │ ║
   ║  └──────────────────────────────────────────────────────────────┘ ║
   ║                              │                                    ║
   ║                              ▼                                    ║
   ║  ┌──────────────────────────────────────────────────────────────┐ ║
   ║  │ 步骤3: appendTextDelta (merge-parts.ts:23-39) 纯函数          │ ║
   ║  │   last = parts[len-1]                                        │ ║
   ║  │   if last.type=="text":                                      │ ║
   ║  │     parts[len-1] = {type:"text", text: last.text+content}    │ ║
   ║  │   else:                                                      │ ║
   ║  │     parts.push({type:"text", text:content})                  │ ║
   ║  │   ★ 连续 delta 缝合同一 text part 尾部                        │ ║
   ║  └──────────────────────────────────────────────────────────────┘ ║
   ╚═══════════════════════════════════════════════════════════════════╝
                              │
                              ▼ messages state 变
   ╔═══════════════════════════════════════════════════════════════════╗
   ║  React 渲染管线                                                   ║
   ║                                                                   ║
   ║  messages (useState)                                              ║
   ║    → adapterMessages (useMemo 双层去重 adapter:1446)              ║
   ║        Layer1: 精确 ID 去重                                       ║
   ║        Layer2: 前 300 字符内容签名去重 (防 #331 崩溃)            ║
   ║    → threadMessages (useMemo convertToThreadMessage adapter:159) ║
   ║        HotPlexMessage → assistant-ui ThreadMessage                ║
   ║        status: streaming → {type:"running"}                       ║
   ║    → useExternalStoreRuntime (ChatContainer:77)                   ║
   ║    → ThreadPrimitive.Messages (thread.tsx:94)                     ║
   ║        message.role==="assistant"? <AssistantMessage>             ║
   ╚═══════════════════════════════════════════════════════════════════╝
                              │
                              ▼
   ╔═══════════════════════════════════════════════════════════════════╗
   ║  AssistantMessage (AssistantMessage.tsx:42)                      ║
   ║  React.memo + 自定义比较函数 (:148-187)                          ║
   ║                                                                   ║
   ║  ★ 性能关键:                                                      ║
   ║    - ID 不同 → 重渲染                                             ║
   ║    - running↔complete 切换 → 强制重渲染 (光标显隐)                ║
   ║    - 逐 part 浅比较 (text 比 text, tool 比 toolName+args+result)  ║
   ║      旧版按 prev.message.content 数组引用比 → 总变 → memo 失效    ║
   ║      新版按值比较 → 仅变动 part 重渲染                             ║
   ║                                                                   ║
   ║  MessagePrimitive.Parts 遍历 parts:                               ║
   ║    p.type=="text" →                                               ║
   ║      <div className={streaming?"streaming-cursor":""}>            ║
   ║        <MarkdownText text={p.text} />                             ║
   ║      </div>                                                       ║
   ║    p.type=="reasoning" → <ReasoningBlock>                         ║
   ║    p.type=="tool-call" → TerminalTool/FileDiffTool/SearchTool...  ║
   ║    p.type=="tool-summary" → 紧凑徽章                              ║
   ╚═══════════════════════════════════════════════════════════════════╝
                              │
                              ▼
   ╔═══════════════════════════════════════════════════════════════════╗
   ║  MarkdownText (MarkdownText.tsx:137)                             ║
   ║  <ReactMarkdown remarkPlugins={[remarkGfm]} components={{...}}>  ║
   ║    代码块: highlight.js (异步 getHighlighter) → CodeBlock        ║
   ║      (复制按钮, >10 行可折叠)                                     ║
   ║    ★ 每次 delta flush 都重新整树解析 markdown                     ║
   ║      (无专门流式 markdown 优化, 靠 react-markdown 容错)           ║
   ║                                                                   ║
   ║  ⑧ 流式光标 = 纯 CSS (globals.css:574-581)                       ║
   ║  .streaming-cursor:last-of-type::after {                         ║
   ║    content: "▍";                                                  ║
   ║    animation: streamingCursor 0.8s steps(2) infinite;            ║
   ║    color: var(--accent-gold);                                    ║
   ║  }                                                                ║
   ║  @keyframes streamingCursor { 0%,100%{opacity:1} 50%{opacity:0}}║
   ╚═══════════════════════════════════════════════════════════════════╝
                              │
                              ▼
                          ⑧ DOM 像素
                  (闪烁的 ▍ 金色光标 + 流式 markdown)
```

---

## 7. 背压与 Reconcile 完整时序

这是整个链路最微妙的部分：delta 在多层都可能被丢弃，但通过 **Done 时的对账机制** 保证最终一致性。

```
  时间轴 ──────────────────────────────────────────────────────────────────────►

  Worker     ┃ Delta1 Delta2 Delta3 ... DeltaN          Done
             ┃   │      │      │           │             │
              ─────────────────────────────────────────────────────────────────
  recvCh256  ┃   ●──────●──────●───────────●─────...─────●
  (cap=256)  ┃   │      │      │           │             │
              ─────────────────────────────────────────────────────────────────
  Bridge     ┃   ├─Clone+turnText+seq+EncodeJSON────────►├─(无 reconcile 标记)
  forward    ┃   │      │      │           │             │
              ─────────────────────────────────────────────────────────────────
  Hub        ┃   ├─trySendBroadcast(非阻塞)──────────────►
  broadcast  ┃   ▼      ▼      ▼           ▼             ▼
  cap=256    ┃  ┌─────────────────────────────────────┐
             ┃  │ Hub.Run 单 goroutine 串行 routeMsg  │
             ┃  └─┬──────┬──────┬─────────┬─────...──┬┘
              ─────────────────────────────────────────────────────────────────
  Conn       ┃    ▼      ▼      ▼         ▼           ▼
  writeCh    ┃   OK     OK     OK       ★丢弃!        OK
  cap=256    ┃   │      │      │        (满)          │
             ┃   │      │      │         │            │
             ┃   │      │      │         ▼            │
             ┃   │      │      │    GatewayDeltasDropped.Add (仅计数)
             ┃   │      │      │         │            │
              ─────────────────────────────────────────────────────────────────
  WS frame   ┃   ●      ●      ●        (丢)          ●
             ┃   │      │      │                      │ DoneData (无 dropped 字段)
             ┃                                                   │
              ────────────────────────────────────────────────────│────────────
  Browser    ┃   │      │      │                      │           │
  同步提交   ┃   ├──appendDelta┤                      │           │
             ┃   │      │      │                      │           │
             ┃   setState       ▼                      ▼           ▼ handleDone
             ┃   appendTextDelta ─────────► 完整流式渲染 (有缺口)  │
             ┃   React re-render                                  │ 无条件触发:
             ┃   Markdown 整树解析                                │   reconcileTurnContent()
             ┃   ▍光标闪烁                                       │     │
              ──────────────────────────────────────────────────────│──────────
  Reconcile  ┃                                                    │     ▼
  (恢复一致) ┃                                                    │ getSessionHistory(sid,limit:5)
             ┃                                                    │   │
             ┃                                                    │   ▼ 取最后 assistant turn
             ┃                                                    │ authoritative content
             ┃                                                    │   │
             ┃                                                    │   ▼ 前缀校验
             ┃                                                    │ if !startsWith(streamed,auth):
             ┃                                                    │   return ← 拉到别的 turn,拒绝 patch
             ┃                                                    │   │
             ┃                                                    │   ▼ 长度校验
             ┃                                                    │ if len(auth)>len(streamed):
             ┃                                                    │   │
             ┃                                                    │   ▼ 多 text part 折叠
             ┃                                                    │ patch: [text,tool-call,text]
             ┃                                                    │   → [mergedAuth,tool-call]
             ┃                                                    │   │
             ┃                                                    │   ▼ setMessages 用权威内容覆盖
             ┃                                                    │ 最终一致 ✓
              ─────────────────────────────────────────────────────────────────
```

### 7.1 无条件 Reconcile

WebChat 在每个 `done` 事件到达时**无条件**调用 `reconcileTurnContent`，从权威 turns 表拉取本 turn 的完整 assistant 内容，与流式累积的文本比对，按需 patch。后端**不**在 done 上附加任何 dropped 标记——droppable 事件丢失是预期行为，不需要跨层信号通知。

代价是每次 done 多一次廉价 REST 调用（`getSessionHistory`）。收益是消除了跨层标记协议的同步负担（后端 sessionDropped map / MarkDropped / GetAndClearDropped / reconcileDroppedDeltas / DoneData.Dropped + 前端条件判断）。

`reconcileTurnContent` 内部三重保护使无条件触发安全：
1. **前缀校验** `full.startsWith(currentText)` —— 拉到别的 turn 则拒绝
2. **长度校验** `full.length <= currentText.length` —— 流式完整则不 patch
3. **cancelled guard** —— unmount 后不写 state

### 7.2 unmount guard

```ts
// hotplex-runtime-adapter.ts:411-412
let cancelled = false;
```

- `reconcileTurnContent` 内部多次检查 `if (cancelled) return`
- effect cleanup: **先 flush 待提交 delta 再设 cancelled**（顺序关键 — 先 flush 让其 `setMessages` 落到仍挂载的实例以保留 streaming 尾巴）

---

## 8. 缓冲容量与可丢弃事件分类

```
   ┌────────────────────────────────────────────────────────────────────┐
   │                       逐节点缓冲容量                              │
   ├──────────────────────────────┬──────┬─────────────────────────────┤
   │ 节点                          │ cap  │ 丢弃策略                    │
   ├──────────────────────────────┼──────┼─────────────────────────────┤
   │ proc.Manager stdout scanner  │ 10MB │ ErrTooLong→ErrCodeWorker...│
   │ base.Conn.recvCh             │ 256  │ TrySend 非阻塞丢弃 delta    │
   │ codexcli subscriber chan     │ 256  │ sendEnvelope 丢弃 delta     │
   │ OCS subscriber chan          │ 256  │ isDroppable 丢弃 delta      │
   │ acp NotificationCh           │ 256  │ 非阻塞 + warn               │
   │ acp RequestCh                │  16  │ 非阻塞 + warn               │
   │ Hub.broadcast                │ 256  │ 阻塞 Hub.Run (HoL)          │
   │ Conn.writeCh                 │ 256  │ ★delta 丢弃 / 关键事件断连 │
   │ pcEntry chan                 │ 56+  │ DropThreshold 丢弃          │
   │ collector.captureC           │ 2048 │ 异步丢(不影响实时流)        │
   │ collector delta flush size   │ 4096 │ size/timer/event 三触发     │
   │ collector delta flush time   │  3s  │ 超时 flush                  │
   └──────────────────────────────┴──────┴─────────────────────────────┘

   ┌────────────────────────────────────────────────────────────────────┐
   │                  不可丢弃 vs 可丢弃 事件分类                       │
   ├─────────────────────────────────────────────────┬──────────────────┤
   │ 不可丢弃 (阻塞至 ctx done / writeCh 满断连)       │ 可丢弃 (静默)    │
   ├─────────────────────────────────────────────────┼──────────────────┤
   │ state / done / error / message / message.start  │ MessageDelta     │
   │ message.end / step / permission*                │ Reasoning        │
   │ question* / elicitation* / tool_call / tool_result │ Raw           │
   │                                                  │                  │
   │ ★ 所有 streaming-append 内容都是可丢弃的：丢失   │                  │
   │   几帧只影响流式观感，done 后权威 turns 表会 reconcile │            │
   └─────────────────────────────────────────────────┴──────────────────┘
```

**所有 streaming-append 内容（text delta / reasoning / raw）统一为可丢弃**：reasoning 在 turns 表本就不持久化（`turn-replay.ts:56-88` 注释），刷新即丢，背压时丢几帧远好于"为了保护 reasoning 而断连导致整个会话中断"。

**⚠ 注意三层背压的不对称**：上表是 **hub 第一层**（broadcast chan 满）和**平台 pcEntry 层**的语义。但 **webchat 的 `*Conn` 第二层**（writeCh 满）有自己的策略：
- `Raw` → 静默丢弃（`trySendData`）
- `delta`/`reasoning`/`state`/`done`/`error`/... → **断连**（`sendData`，慢客户端断开后重连 replay）

这是有意为之：webchat 是本地可控 WS 客户端，断连 + 重连 replay 比"静默丢 delta 让 UI 缺一角"更合适。远程平台（feishu/slack）则用 droppable + coalescing，因为断连代价更高。详见 §5。

---

## 9. 持久化与平台 Coalescing 旁路

### 9.1 eventstore delta 聚合

`MessageDelta` **不在 events 表单独存储**。`StorableTypes`（`collector.go:26-45`）不含 `MessageDelta/MessageStart/MessageEnd`。delta 只通过 accumulator 合并成 `message` 后存。

```
   Bridge.captureForwardedEvent (bridge_forward.go:492)
     → Collector.CaptureDeltaString (collector.go:487)  ← delta 直接字符串
          → acc.appendRaw (collector.go:539)             ← 累积到 strings.Builder
          → if content.Len() >= 4096: send(toRequest)    ← size 触发 flush
          → else: 留在内存等 timer/event flush
   Collector.runWriter (collector.go:362)  ← 单 writer goroutine
     → ticker(1s): flushTimedOutAccumulators (>3s 累积器 flush) + flushBatch
     → batch >= 100: flushBatch
   flushBatch (collector.go:435) → BeginTx → AppendTurn/Append → Commit
```

**delta 聚合的三种 flush 触发**（`collector.go:82-86` 注释）：

- **Size**（热路径，同步）：content ≥ `deltaFlushSize=4096` bytes
- **Timer**（runWriter ticker）：accumulator age ≥ `deltaFlushInterval=3s`
- **Event**（热路径，同步）：MessageEnd 或下个 storable 事件 → `flushBoth`

聚合结果格式（`collector.go:550-565` `toRequest`）：

```json
{"content": "<合并的全部 delta>", "merged_count": N, "seq_range": [firstSeq, lastSeq]}
```

存为**单个** `StoredEvent`，type=`message`，seq=firstSeq，direction=outbound。

**先转发后存**（`bridge_forward.go:270` SendToSession 在 `:274` captureForwardedEvent 之前），且存是异步（collector 内部 channel + writer goroutine），不阻塞转发。

### 9.2 pcEntry 平台 delta coalescing

平台 SDK（Slack/飞书）的 HTTP API 调用慢，pcEntry 用独立 writeLoop goroutine（`platform_writer.go:170-249`）做合并：

- 连续 droppable 事件（MessageDelta/Raw）的 content 累积到 `strings.Builder db`
- 触发 flush：rune 数 ≥ `CoalesceSize`（默认 200）或 timer 到期（`CoalesceIntvl` 默认 120ms）
- flush 时合成单个 merged MessageDelta envelope → `pc.WriteCtx`
- 非 droppable 事件先 flush pending delta，再原样转发

这是 delta 在 gateway 内部的**第三个变换**（前两个：events.Clone、CaptureDeltaString 聚合持久化）。

---

## 10. 关键源码索引

按链路顺序排列，所有路径前缀为`仓库根`。

### 协议层

| 关注点                                | 位置                            |
| ------------------------------------- | ------------------------------- |
| `MessageDelta` Kind 常量              | `pkg/events/events.go:23`       |
| `MessageDeltaData{MessageID,Content}` | `pkg/events/events.go:204-207`  |
| NDJSON 编码 + U+2028 转义             | `pkg/aep/codec.go:27-49`        |
| seq 真相源 (per-session atomic)       | `internal/gateway/seq.go:29-32` |

### Worker 层

| 关注点                            | 位置                                                  |
| --------------------------------- | ----------------------------------------------------- |
| claudecode delta 转换 (mapStream) | `internal/worker/claudecode/mapper.go:126-163`        |
| claudecode 后缀差分               | `internal/worker/claudecode/mapper.go:348-383`        |
| codexcli drift 检测/纠正          | `internal/worker/codexcli/mapper.go:858-920`          |
| codexcli sendEnvelope 背压        | `internal/worker/codexcli/manager.go:996-1019`        |
| OCS handlePartDelta (纯增量)      | `internal/worker/opencodeserver/converter.go:175-213` |
| acp mapAgentMessageChunkText      | `internal/worker/acp/mapper.go:204-225`               |
| base.Conn.TrySend (非阻塞)        | `internal/worker/base/conn.go:68-75`                  |
| stdin 写背压 WriteWithCtxBounded  | `internal/worker/base/write_ctx.go:16-27`             |

### Bridge 层

| 关注点                             | 位置                                         |
| ---------------------------------- | -------------------------------------------- |
| forwardEvents 主循环               | `internal/gateway/bridge_forward.go:156-159` |
| processForwardedEvent (12 步)      | `internal/gateway/bridge_forward.go:186-313` |
| events.Clone 深拷贝                | `:248`                                       |
| 先转发后持久化顺序                 | `:270` → `:274`                              |
| captureForwardedEvent              | `:492-501`                                   |
| forwardContext 单 goroutine 所有权 | `:30-50`                                     |

### Hub 层

| 关注点                                    | 位置                            |
| ----------------------------------------- | ------------------------------- |
| isDroppable(MessageDelta\|Reasoning\|Raw) | `internal/gateway/hub.go:40-44` |
| sendBroadcast (guaranteed，阻塞)          | `:308-318`                      |
| trySendBroadcast (droppable，非阻塞)      | `:324-334`                      |
| SendToSession seq 盖戳                    | `:351-353`                      |
| broadcast chan cap=256                    | `:151`                          |
| Hub.Run routeMessage                      | `:522-579`                      |
| EncodeJSON 一次性编码                     | `:553`                          |
| JoinSession 去重 (只留最新)               | `:212-235`                      |

### Conn 层

| 关注点                          | 位置                           |
| ------------------------------- | ------------------------------ |
| writeCh cap=256                 | `internal/gateway/conn.go:113` |
| trySendData (delta 丢弃)        | `:814-825`                     |
| sendData (非 delta 断连)        | `:793-803`                     |
| writeDispatch 分流              | `:780-789`                     |
| WritePump WS 写 (writeWait=10s) | `:689-736`                     |
| init 阶段缓冲                   | `:869-910`                     |
| performInit 四阶段              | `:243-266`                     |

### 持久化层

| 关注点                               | 位置                                       |
| ------------------------------------ | ------------------------------------------ |
| collector CaptureDeltaString         | `internal/eventstore/collector.go:487-503` |
| delta flush 三触发 (size/time/event) | `:82-86,417-433`                           |
| 聚合格式 (merged_count/seq_range)    | `:550-565`                                 |

### 平台层

| 关注点                                   | 位置                                          |
| ---------------------------------------- | --------------------------------------------- |
| pcEntry delta coalescing (200rune/120ms) | `internal/gateway/platform_writer.go:170-249` |
| pcEntry 自己的背压                       | `:111-152`                                    |

### WebChat 层

| 关注点                             | 位置                                                            |
| ---------------------------------- | --------------------------------------------------------------- |
| WS onMessage + deserializeEnvelope | `webchat/lib/ai-sdk-transport/client/browser-client.ts:237-247` |
| delta emit                         | `:369-371`                                                      |
| handleDelta 同步提交               | `webchat/lib/adapters/hotplex-runtime-adapter.ts:504-508`       |
| appendDelta                        | `:435-459`                                                      |
| appendTextDelta 纯函数             | `webchat/lib/adapters/merge-parts.ts:23-39`                     |
| reconcileTurnContent (无条件触发)  | `hotplex-runtime-adapter.ts:671-736`                            |
| unmount cancelled guard            | `:411-412`                                                      |
| streamingFallbackId (#331)         | `:432,553-563,962-969`                                          |
| adapterMessages 双层去重           | `:1446-1489`                                                    |
| HotPlexMessage 数据结构            | `webchat/lib/types/message.ts:11-17`                            |
| MessagePart 联合类型               | `webchat/lib/types/message-parts.ts:9-44`                       |
| AssistantMessage memo + 比较       | `webchat/components/assistant-ui/AssistantMessage.tsx:148-187`  |
| MarkdownText                       | `webchat/components/assistant-ui/MarkdownText.tsx:137-221`      |
| 流式光标 CSS                       | `webchat/app/globals.css:574-581`                               |

---

## 11. 一句话浓缩

> Worker 把底层 AI 进程的协议流（NDJSON / JSON-RPC / SSE）解析并统一成 `MessageDelta{MessageID,Content}`，经 `recvCh(256) → Bridge 单 goroutine 串行化(Clone+turnText+seq+EncodeJSON) → Hub trySendBroadcast(256,非阻塞) → per-conn writeCh(256)` 投递到 WS。droppable 事件（`MessageDelta`/`Reasoning`/`Raw`）在 Hub broadcast 满时静默丢弃，保证 Hub.Run 永不被单慢 session 阻塞；webchat `*Conn` 对 delta/reasoning 走 guaranteed 路径（慢则断连重连 replay），平台 pcEntry 走 droppable + coalescing；非 droppable 事件（state/done/error）全程 guaranteed。WebChat 用 React 18 自动批处理的同步 `setMessages` 把 delta 缝到最后一个 streaming text part，并在 done 时**无条件**调 `reconcileTurnContent` 拉权威 turns 表经**前缀+长度校验**后 patch，最终经 `react-markdown` 整树重解析 + CSS `::after` 闪烁块实现像素级流式渲染。

---

## 12. 设计准则

本链路遵循三条核心准则，确保流式 delta 的简单性与鲁棒性。

### 12.1 关键数据路径不依赖浏览器节流 API

delta 提交采用同步 `setMessages`，依赖 React 18 自动批处理合并同一 microtask 内的多次更新。`requestAnimationFrame` / `setTimeout` / `Promise` 等浏览器节流 API 会在后台 tab 被暂停或降频，而 WebSocket 仍持续接收 delta——节流 API 一旦用于累积缓冲，后台 tab 期间数据会无限堆积且 flush 永不触发，切回前台时丢失累积的尾巴。因此这些 API 不用于 delta 数据路径。

### 12.2 不为 cosmetic 数据引入跨层标记协议

delta 是流式展示数据（cosmetic），权威内容在 turns 表。消费者（前端）具备自校验能力（前缀+长度比对），应在 done 时**无条件校验**，而非依赖后端跨层传递"该校验了"的信号。跨层标记协议（如 dropped bool 字段）需要 6 处组件跨 Go/TS 同步，复杂度成本远超它省下的一次廉价 REST 调用，且任一处失同步即失效。

### 12.3 不用阻塞串行化保护可丢弃事件

droppable 事件在通道满时应立即用 `select default` 丢弃。若改用阻塞 send"尽量不丢"，单一慢 session 会卡住 Hub.Run 单 goroutine，导致无关 session 的事件全部堆积，最终在 worker 层 recvCh 触发更大规模丢弃——把"单 session 背压"扩散成"全网 delta 丢失"。直接丢弃 + done 时 reconcile 才是正确的背压语义。
