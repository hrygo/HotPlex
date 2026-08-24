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

## 最小证据路径

先记录用户症状、Session ID（如有）、Worker 类型、问题时间窗，以及当前部署版本。
然后按症状选择最窄的只读检查，不为了“完整”执行所有命令：

| 症状 | 第一组证据 | 停止或继续条件 |
| --- | --- | --- |
| Gateway 或服务不可达 | `hotplex status --format json`、`hotplex service status --json` | Gateway 未运行时停止在服务层，不用后续 Session 证据覆盖它 |
| Worker 运行但没有反馈 | 运行状态、只读 Session 视图、日志时间线、反馈链指标 | 只有确认存在活动 Session 后才读取 `feedback-chain.md` |
| Session 卡住、重复或状态异常 | Session 状态、Worker run/generation、相关 fence 或完成事件 | 证据不足以区分根因时列出候选，不用 mutation 试错 |
| 配置或依赖异常 | `hotplex doctor --json`，必要时 `hotplex security --json` | 只读检查已解释症状时停止，不自行执行 `--fix` |
| 需要时间线细节 | `hotplex service logs --lines <N>` 或已授权的只读日志接口 | 先用当前 `--help` 确认参数，并限定时间窗口和脱敏范围 |

`status` 的格式参数、其他命令的 JSON 参数和日志行数参数都必须先以当前
`--help` 确认；上表是当前公共命令面的起点，不是跨版本固定契约。

每组证据至少回答：组件是否存活、哪个 Session/Worker/run 受影响、上游最后一条
事件和下游最后一条事件分别是什么、是否存在最终 `done/error`、还有哪个事实未验证。
没有这些字段时，不要把“没有日志”直接写成“已确认丢失”。

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

最小报告应包含：症状与时间窗、已执行的只读检查、观测事实、根因链或候选根因、
影响范围、证据缺口和下一步。若下一步需要重启、终止 Session、修改配置、读取凭据
或发送测试消息，单独标记为需要授权，不把它写成已执行动作。
