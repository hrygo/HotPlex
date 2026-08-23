# Runtime diagnosis

## 建立只读基线

先确认当前安装版本暴露的命令：

    hotplex status --help
    hotplex doctor --help
    hotplex security --help
    hotplex service status --help

根据症状选择最窄检查。`status` 和 `service status` 确认运行状态，`doctor` 检查
配置与依赖，`security` 检查安全姿态。命令不存在或参数与文档不同，以安装版本的
`--help` 为准并报告差异，不猜测结果。

若需要更深证据，按以下顺序推进：

1. **进程与监听**：确认 Gateway、Admin 和相关 Worker 是否存活、PID 是否匹配、
   监听地址是否符合实际配置。进程不存在时停止，后续状态可能已经过期。
2. **Worker 与 Session**：通过已授权的只读 Admin client 检查
   `/admin/health/workers`、`/admin/sessions` 和必要的
   `/admin/debug/sessions/{id}`。不要用终止或修复端点完成诊断。
3. **日志时间线**：优先使用 `hotplex service logs` 或已授权的只读日志接口，按
   Session、Worker 和时间窗口重建事件顺序。保留错误前后的上下文，不用截断后的
   片段证明负面结论。
4. **只读存储检查**：仅在 Admin 视图不可用且确有必要时，以只读方式查询运行库。
   不修改 SQLite/PostgreSQL 状态，不复制私人消息、凭据或完整 payload。
5. **源码交叉验证**：仅当日志仍无法解释行为且源码可用时，用错误字面量定位当前
   实现，再检查调用链和最近变更。源码中的可能路径不能替代运行时证据。

手动进程、端口和文件命令需适配当前平台；不要把 Linux、macOS 或 Windows 的服务
管理器语义互相套用。优先使用 HotPlex 的跨平台 CLI 表面。

## 停止条件

- Gateway 未运行：报告进程或服务层问题并停止。
- 没有相关活动 Session：跳过反馈链检查，转向日志或配置证据。
- 只读证据不足以区分多个根因：列出候选及缺失证据，不执行 mutation 来试错。
- 下一步涉及重启、终止、配置写入、更新或凭据访问：停止并请求相应授权。

## 报告

正常组件简述即可；异常按严重度给出“观测 → 推断 → 影响 → 下一步”。数值必须附带
采样时间和上下文，不使用固定阈值替代现场基线。
