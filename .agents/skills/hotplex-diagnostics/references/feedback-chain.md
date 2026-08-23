# Feedback-chain diagnosis

当 Worker 有持续活动而用户端没有增量或最终反馈时，比较同一 Session、同一时间窗的
两条时间线：

1. Worker 产出与 Gateway 接收的事件；
2. Gateway 路由、平台写入与客户端接收的事件。

仅在已授权的只读 Admin client 可用时检查 `/admin/health/workers`、
`/admin/debug/sessions/{id}`、`/admin/logs` 和 `/admin/metrics`。查询指标的实际名称和
标签后再引用；不要凭文档记忆假定当前版本一定暴露某个计数器。

## 定位边界

沿当前源码或日志中实际观察到的链路定位，不硬编码某个 Worker 的内部实现：

```text
Worker event -> Gateway bridge -> session routing -> platform writer -> client
```

可用证据分类症状：

- **pipeline stall**：上游持续有事件，下游没有对应路由或写入证据；
- **backpressure drop**：指标或完成事件明确记录了增量丢弃；
- **adapter failure**：平台写入、刷新、限流或内容约束失败；
- **client disconnect**：连接替换、心跳超时或客户端断开解释了缺口。

“上游有日志、下游没有日志”只能支持链路缺口的推断；若下游路径本身不记录成功事件，
需要指标、连接状态或源码行为补强，不能把日志缺失直接表述为已确认丢弃。

## 时间线方法

- 固定 Session ID、Worker run/generation 和问题时间窗，避免把重连后的不同轮次混在一起。
- 对齐 Worker event、Gateway sequence、路由连接和平台写入的时间戳。
- 检查背压、连接替换、TTL、刷新失败和最终 `done/error` 是否共同解释缺口。
- 遇到旧连接或旧 generation 的事件时，先验证所有权和顺序，再判断是否为丢失。

诊断结束后给出最窄根因链和证据缺口。不要为了验证猜测而重启服务、终止 Session、
修改队列参数或发送测试消息；这些都是需要单独授权的操作。
