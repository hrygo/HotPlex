# Feishu Streaming Card TTL Rotation — Spec

**Issue**: [#839](https://github.com/hrygo/hotplex/issues/839)
**Status**: Implementing
**Owner**: 黄飞虹

---

## 1. 背景

飞书 CardKit streaming 有 **10 分钟服务端硬限制**：超时后服务端强制关闭 streaming mode
（错误 `200850: card streaming timeout` / `300309: streaming mode is closed`）。

HotPlex 的 `StreamTTL = 500s`（8.3min）设计为"到期前主动轮转新卡"。
但当前轮转检查在 `writeContent()` 开头（`conn.go:517`），**只在 worker 产出 delta 时触发**。

## 2. 根因（运行日志证据，2026-07-04 17:00–19:06，v1.30.2）

| 指标 | 值 | 含义 |
|------|----|------|
| session created | 9 | 9 个独立 turn |
| card_id 总数 | 13 | 多出 4 张轮转卡 |
| `streaming card rotated` | 5 | TTL 轮转触发 5 次 |
| 飞书 200850/300309 | 2 | 服务端强制关闭 2 次 |

worker 在 delta 间隙（tool 思考、长计算）时 `writeContent` 不被调用 → 轮转错过 ~100s 窗口
→ 飞书 10min 先关闭 streaming → 卡片卡在"生成中"、final flush 降级/失败。

铁证：卡1 存活 `17:00:52 → 17:11:06` = **10min14s**，撞飞书硬限制。

## 3. 设计

### 3.1 核心方案：主动 timer 触发轮转

`StreamingCardController` 进入 `PhaseStreaming` 时启动 `time.AfterFunc(StreamTTL)` 定时器。
到期主动触发 conn 层轮转回调，**不再依赖 delta 到达**。

### 3.2 改动点

**`streaming.go` — StreamingCardController**：
- 新增字段：`onExpired func()`、`rotateTimer *time.Timer`
- 新增 `SetOnExpired(fn)` —— conn 在创建 controller 后注入回调
- `createAndEnable()` transition(PhaseStreaming) 成功后启动 `rotateTimer = AfterFunc(StreamTTL, c.triggerRotation)`
- `triggerRotation()`：`recover` 包裹 + 调 `onExpired`（不阻塞 timer 线程）
- `Close()` / `Abort()` 中 `Stop()` rotateTimer（与 stopFlushLoop 一致的清理时机）

**`conn.go` — FeishuConn**：
- 新增 `rotateMu sync.Mutex` —— 串行化轮转（timer vs writeContent 并发请求）
- 抽离 `rotateStreamingCard(ctx) (newCtrl, rotated)` —— 从 `writeContent:515-548` 提取，复用
- 新增 `onCardExpired()` —— timer 回调入口，`go rotateStreamingCard` 独立 goroutine 避免阻塞
- `writeContent` 替换内联轮转逻辑为 `rotateStreamingCard` 调用
- 3 个 controller 创建点（`handler.go:227`、`conn.go:122 resetStreamCtrl`、`conn.go:539`）
  在 `EnableStreaming` / 创建后 `SetOnExpired(c.onCardExpired)` 注入回调

### 3.3 并发安全（重点 — 见观察 #8465 历史 data race）

| 风险 | 缓解 |
|------|------|
| timer 与 writeContent 同时触发轮转 | `rotateMu` 串行化 + 进入后 `Expired()` 复检 |
| Close 持锁阻塞 writeContent | Close 在 `rotateMu` 下但**不持 `c.mu`**；仅替换 ctrl 时短暂持 `c.mu` |
| 旧 ctrl 已被并发替换 | 替换前校验 `c.streamCtrl == cur`，否则放弃 |
| timer 在 Close 后触发 | `Close()` Stop timer；`triggerRotation` 检查 phase ≥ Completed 直接返回 |

### 3.4 不变式

- 每 controller 至多触发一次主动轮转（Close 后 timer Stop）
- 轮转后新 controller 自动注入 onExpired，形成链式主动轮转
- 短任务（<StreamTTL）行为完全不变（timer 被 Close 提前 Stop）

## 4. 实施

1. `streaming.go`：加 timer + onExpired + triggerRotation + Stop 时机
2. `conn.go`：抽离 `rotateStreamingCard` + `onCardExpired` + 注入回调
3. 测试：`TestStreamingCard_ProactiveRotation` — 模拟 TTL 到期无 delta，验证主动轮转
4. `make check` + PR

## 5. 验收

- ✅ 长任务（>8.3min）delta 间隙时 ~500s 主动轮转，不再出现 200850/300309
- ✅ 短任务行为不变（单卡 seq 递增）
- ✅ timer 回调与 writeContent 不重复轮转（rotateMu + 复检）
- ✅ `-race` 通过
