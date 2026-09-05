---
title: 十二组合端到端证据模板
weight: 90
description: 为同一候选提交记录 Source、Test、Live 与原生差异。
---

# 十二组合端到端证据模板

每次验收复制本文件。填写候选 commit、验证时间、实际 Worker / agent 版本和测试环境标识。初始状态不是通过；只有完整操作证据支持的单项才能改为 PASS。

| ID | 平台 | Worker | Source | Test | Live | 功能失败或降级证据 |
| --- | --- | --- | --- | --- | --- | --- |
| F-C | 飞书 | claude_code | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| F-O | 飞书 | opencode_server | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| F-X | 飞书 | codex_cli | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| F-A | 飞书 | acp | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| S-C | Slack | claude_code | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| S-O | Slack | opencode_server | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| S-X | Slack | codex_cli | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| S-A | Slack | acp | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| W-C | WebChat | claude_code | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| W-O | WebChat | opencode_server | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| W-X | WebChat | codex_cli | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |
| W-A | WebChat | acp | NOT_RUN | NOT_RUN | NOT_RUN | 未执行 |

每个组合另附功能明细：输入、工具、停止、下一轮、补充、权限、问答、表单、命令模型、附件、语音、恢复、重置、故障处理。仅完成部分功能时保留部分结果，不将整行改为全部通过。

执行步骤和判定标准见 [验收手册](../../guides/developer/platform-worker-e2e-validation.md)。
