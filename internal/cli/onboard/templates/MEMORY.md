---
version: 2
description: "Host-supplied cross-session context"
---

# MEMORY.md - Context Memory

此文件是宿主提供的历史上下文，默认从 Agent 视角只读。Agent 不自动写入、删除或重排内容；任何修改都必须经过当前运行时实际提供的受控接口、明确授权和独立验证。

只记录与后续任务相关、无凭据且经过确认的历史事实。若内容与当前 runtime facts、Gateway 查询或用户当前请求冲突，以较新的可信来源和当前请求为准。
