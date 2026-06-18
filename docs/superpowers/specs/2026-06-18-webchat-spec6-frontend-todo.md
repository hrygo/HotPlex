# WebChat 多租户 spec ⑥ — 前端一等公民化 · 待办工作清单

**日期**: 2026-06-18
**状态**: 待办（brainstorm 进行中）
**跟踪 issue**: [#760](https://github.com/hrygo/hotplex/issues/760)
**分支**: `feat/webchat-spec6-frontend`
**线路图**: [`WebChat-Multitenancy-Roadmap-Spec.md`](../../specs/WebChat-Multitenancy-Roadmap-Spec.md)

---

## 1. 定位

spec ⑥ 是 WebChat 多租户线路图的**最后一个 spec（集大成）**。spec ①-⑤ 后端已全部合入（#746 / #748 / #753 / #757 / #755）。本 spec 将 WebChat 前端从"匿名 SPA"升级为完整多租户一等公民 UI，同时解除 spec ① R9 review 挂账的后端潜伏接线。

> 后端"已合入" ≠ "端到端可用"。本清单经代码核实，区分"已就绪 / 潜伏 / 待对齐"。

---

## 2. 已核实现状

### 前端技术栈（无需演进 — 决策点 #4 已由现状回答）

Next.js 16.2 + React 19.2 + assistant-ui 0.14 + Tailwind 4 + TypeScript 6 + framer-motion + nuqs + playwright。详见 `webchat/package.json`。继续 Next.js + assistant-ui，不重构。

### 已有独立 admin 登录体系（不复用）

`app/admin/login/page.tsx` + `hooks/use-admin-auth.ts` + `lib/api/admin-client.ts`。线路图明确其"与聊天功能无关"。spec ⑥ 为**聊天主入口 `app/page.tsx`**（当前匿名 SPA）新建用户登录。

### 后端 API 契约

| 契约 | 状态 | 代码位置 |
|---|---|---|
| `/api/auth/{login,logout,me,accept-invite}` | ✅ 已注册 | `cmd/hotplex/routes.go:209-212` |
| `/api/auth/oauth/{providers,login,callback}` | ✅ 已注册（#757） | `cmd/hotplex/routes.go:218-220` |
| `POST /api/sessions` 强制 workspace_id | ✅ 后端已强制 | `internal/gateway/api.go:237-240` |
| `/api/workspaces*` CRUD | 🔴 handler 已写、**路由未注册** | `internal/gateway/workspace_handlers.go` 五方法齐 + `routes.go` 缺注册 |

---

## 3. 待办工作清单

### A. 后端潜伏接线（端到端硬前提）

| ID | 待办 | 优先级 | 代码位置 | 来源 |
|---|---|---|---|---|
| A1 | 注册 `/api/workspaces*` CRUD 路由 | 🔴 阻塞前端 B2 | `workspace_handlers.go` Create/List/Get/Update/Delete 已实现，`routes.go` 未 mux.Handle | spec①R9#1 |
| A2 | WS init 创建 session 绑定 workspace | 🔴 | `internal/gateway/conn.go` 无任何 WorkspaceID（绕过隔离 + session key 方案3 分叉） | spec①R9#4 |
| A3 | disable 用户 per-request 状态校验 | 🟡 security | `internal/security/auth.go:80 AuthenticateRequest` 不查 `users.status`；cookie 7d / API key 永久有效期内 disable 用户仍可访问 | spec①R9#2 |
| A4 | cookie HMAC secret 持久化 | 🟡 reliability | `internal/security/cookie.go:52` 仍 `rand.Read` 内存生成，重启踢全部用户、多实例不共享 | spec①R9#3 |
| A5 | webchat 前端发送 workspace_id 对齐 REST 强制 | ⚠️ | 后端 `api.go:237` 已强制，前端未发送 → 当前 webchat REST 建会话可能 400 | spec①R9#5 |

### B. 前端主体（spec ⑥ 核心）

| ID | 待办 | 消费契约 |
|---|---|---|
| B1 | 登录页（内建账号 + OAuth 入口） | `GET /api/auth/oauth/providers` |
| B2 | workspace 切换 UI | A1 的 `/api/workspaces` CRUD |
| B3 | worker 选择 UI | spec ③ `worker_preference`（`PATCH /api/workspaces/{id}`） |
| B4 | agent-configs 编辑 UI | spec ② `agent_config_overrides` |
| B5 | OAuth post-login routing + 错误渲染 | `internal/gateway/oauth_handlers.go:175,254` 留有 `spec ⑥` 注释 |

### C. 文档同步（可立即做，不阻塞）

- **C1** `docs/specs/README.md` 索引滞后：Roadmap/Foundation 仍标 `proposed/draft 0%`（实际完成）；缺 spec ②③④⑤ 四个子 spec 索引行；状态统计失真
- **C2** `docs/specs/WebChat-Multitenancy-Roadmap-Spec.md` 正文 §3 阶段B表格 spec ④ 未标 ✅、§4 spec ④ 详述未标完成（与头部 L4 已更新不一致）

### D. 遗留 / 升级时处理

- **D1** migration 018 旧 API-key WS 会话 `DeriveSessionKey` 派生键断裂（升级孤儿行）
- **D2** spec ① 增量：迁移验证测试 / 旧 webchat 会话清理 / e2e 集成测试

---

## 4. brainstorm 待决策点

启动实现前需在 brainstorm 中明确（一次一个）：

1. **前端技术栈** — ✅ 已定：继续 Next.js + assistant-ui，不重构
2. **A 组归属** — A1-A5 接线放进 spec ⑥ PR 内，还是后端 PR 先行？
3. **登录态迁移** — 现有匿名 `webchat_user` 访问如何过渡（强制登录 vs 渐进式）？
4. **workspace 切换与会话连续性** — 切 workspace = 新会话（`DeriveSessionKey` 已如此），UI 如何表达？
5. **登录页与 admin 登录关系** — 完全独立两套，还是统一登录入口分流到 admin/chat？
6. **审计日志** — 多租户用户/workspace 操作是否审计？（路线图决策点 #5）

---

## 5. 推进路径

```
brainstorm（决策点 2-6）→ 设计文档（docs/superpowers/specs/）
  → writing-plans（实现计划）→ 实现
  ├─ A 组接线（后端，阻塞 B2/B5）
  ├─ B 组前端主体
  ├─ C 组文档同步（可穿插）
  └─ D 组遗留（升级时）
```

## 6. 参考

- 路线图：`docs/specs/WebChat-Multitenancy-Roadmap-Spec.md`
- spec ① 设计：`docs/specs/WebChat-Multitenancy-Foundation-Design-Spec.md`
- 跟踪 issue：[#760](https://github.com/hrygo/hotplex/issues/760)
- 已合入：#746(①) · #748(②) · #753(③) · #757(④) · #755(⑤)
