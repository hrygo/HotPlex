# 飞书 × Claude Code 消息链可靠性、数据治理与安全加固设计

**状态**：Approved  
**日期**：2026-08-03  
**Epic**：[#941](https://github.com/hrygo/hotplex/issues/941)

## 1. 目标

加固飞书消息从接入、排队、Gateway 投递、Claude Code 执行到飞书终态展示的完整链路，同时保留审计原文明文，降低无治理的数据副本、消息丢失、停止失效、资源耗尽、终态不可见和高权限误用风险。

## 2. 已批准决策

1. 审计存储继续保留消息原文明文，作为长期合规与追溯真相源。
2. 飞书停止指令必须进入 AEP `control.stop`，只停止当前 turn，不销毁 session。
3. ChatQueue 的 worker 生命周期与任务入队必须原子化；入队失败必须回滚本次去重登记。
4. 媒体下载必须在流式读取中执行 10 MiB 硬限制，临时目录和文件采用最小权限。
5. 最终 CardKit、IM Patch、header update 失败必须对上游可见、可计量，并尝试静态文本兜底。
6. Claude Code `bypass` 权限模式增加显式 doctor 风险检查与文档，不自动修改现网配置。

## 3. 数据副本模型

### 3.1 允许的受控副本

| 存储层 | 职责 | 原文明文 | 默认时限 |
| --- | --- | --- | --- |
| Audit user activity | 合规、追责、完整原文查询 | 保留 | `audit.retention` |
| Event store | 会话历史、恢复、协议重放 | 保留 | `events.retention` |

两者不是同一用途：audit 是长期治理记录，event store 是短期运行事实。它们必须分别执行自己的留存策略。

### 3.2 禁止的无治理副本

INFO 日志不得输出 `Envelope.Event.Data`、prompt 或消息正文。日志只记录：事件类型、session、seq、数据大小和正文 SHA-256 短指纹。这样既能关联排障，又不会形成第三份无独立留存策略的正文。

### 3.3 留存解耦

现有 `EffectiveEventsRetention` 以 `max(events.retention, audit.full_content_retention)` 延长事件存储，但消息审计记录本身已经包含原文，且当前 `message.inbound` 没有依赖 event reference 才能读取正文。新语义只使用 `events.retention`；`audit.full_content_retention` 保留为兼容配置字段，但不再延长事件/turn 副本。

该变更不删除或脱敏 audit 原文，也不批量修改历史数据；只影响后续 GC 的事件/turn 留存窗口。

## 4. 停止链路

`DetectCommand` 将所有 abort trigger 规范化为 `ControlActionStop`。飞书和 Slack 均通过既有 `handleTextControlCommand` 建立 control envelope，由 Gateway 完成 ownership 校验、`Worker.StopCurrentTurn`、execution runtime 收敛，并发出 `done.reason=stopped_by_user`。

平台本地的流式任务取消仍由收到 terminal/done 后的连接逻辑负责，不能替代 Worker stop。

## 5. ChatQueue 与去重

- `ChatQueue` 增加 closed 状态。
- 查找/创建 worker、确认 worker 可接收、非阻塞入队在同一队列锁保护下完成。
- idle worker 只能在持有队列锁且确认自己仍是 map 当前实例后删除。
- `Close` 在锁内标记关闭并关闭每个 worker channel，后续 Enqueue 返回稳定哨兵错误，不 panic。
- dedup 提供条件删除：仅当记录仍对应本次登记时回滚，避免误删后续记录。
- 飞书 handler 在转换、gate 或 enqueue 失败时回滚本次去重登记；成功入队后保持去重。

## 6. 媒体边界

- 使用 `io.LimitReader(mediaMaxSize+1)` 或等价有界读取，超过上限立即返回明确错误。
- 媒体目录权限 `0700`，文件权限 `0600`。
- 文件名继续使用 `filepath.Base` 和资源 key 组合，禁止路径穿越。
- 失败路径不留下部分文件。

## 7. 终态投递

- `StreamingCardController.Close` 聚合最终 flush、disable streaming、header update 错误。
- CardKit 失败后继续 IM Patch；两者都失败时增加终态失败指标并返回错误。
- 上层连接收到 Close 错误后使用独立静态文本消息发送简短终态兜底；兜底失败也记录指标和结构化日志。
- 成功呈现内容但仅 header 修饰失败时仍返回可识别错误，让上层计量但避免重复完整正文。
- 新指标必须通过 observability 的 lazy `sync.Once` accessor 注册并写入 metrics 文档。

## 8. `bypass` 风险检查

新增 `worker.claude_bypass_mode` doctor checker：解析有效配置及环境覆盖，若 Claude Code 的有效默认或平台 worker 配置为 `bypass`，输出 Warn、风险说明和收紧建议。检查只读，不提供自动修复，不修改生产配置。

## 9. 兼容性与非目标

- 不改变 AEP wire contract。
- 不迁移、不删除、不脱敏历史 audit 原文。
- 不自动切换现网 permission mode。
- 不把 `unknown` 输入状态视作可安全重投。
- Linux、macOS、Windows 都不得引入平台专属路径或信号假设。

## 10. 验收

- audit 测试证明原文明文仍保留；日志测试证明正文不出现。
- event retention 测试证明不再被 audit full-content window 延长。
- stop 解析、飞书路由和 Gateway `stopped_by_user` 闭环测试通过。
- ChatQueue queue-full、idle、Close、并发 race 和 dedup rollback 测试通过。
- 10 MiB 边界、超限流式终止、`0700/0600` 权限测试通过。
- CardKit/IM Patch/header/兜底失败注入测试与指标测试通过。
- bypass checker 在 bypass/workspace/无配置场景下结果正确。
- 相关包 `go test -count=1 -race` 与全量 `make check` 通过。
