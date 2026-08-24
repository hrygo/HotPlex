---
name: hotplex-diagnostics
description: 深入诊断 HotPlex Gateway、Worker、Session、日志或反馈链异常。普通 status/doctor/security/config 只读检查属于 hotplex-cli；安装、更新、重启、配置写入和 Admin 变更属于 hotplex-operator。
---

# HotPlex 诊断

本 Skill 只授权读取和分析。诊断请求不授权修复、终止 Session、读取凭据、创建
Issue 或改变任何外部状态。需要受保护的 Admin 证据时，只使用已经获得授权的
Admin client 或用户提供的凭据上下文；不要读取或打印 token、cookie、完整环境文件
和私人 payload。

## 路由

- Gateway、Worker、Session、进程或日志异常：读取
  [runtime-diagnosis.md](references/runtime-diagnosis.md)。
- Worker 仍在运行但用户收不到增量、流式反馈中断或疑似静默丢弃：再读取
  [feedback-chain.md](references/feedback-chain.md)。
- 仅需运行 `hotplex status`、`doctor`、`security` 或 `config`：改用
  `hotplex-cli`。
- 结论需要安装、更新、重启、配置写入或 Admin mutation 才能验证：停止诊断，
  报告缺少的 operator 授权，不要自行转入修复。

## 证据原则

从用户症状开始，逐层缩小范围；优先使用当前安装版本的 `--help`、只读 CLI 和
只读 Admin 视图，再用日志、进程、只读数据库或源码补充。每个结论区分观测事实、
证据支持的推断和未验证假设。某层已经解释症状时停止，不为完成清单继续扩张范围。

输出应给出根因链、影响、证据和下一步；只报告实际执行的检查。若用户希望修复、
提交 Issue 或进行其他外部操作，先取得该具体动作的明确授权。
