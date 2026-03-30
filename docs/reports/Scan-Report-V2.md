# 架构对齐扫描报告 (Phase 2)

在前序完成核心 Gateway 生命周期、协议时序与僵死进程等底层框架的闭环修复后，针对其余的（非 Worker 专属）配套周边特性的进行二次全量扫荡。

二次扫描结果显示：原架构设计对于**“零信任安全”**和**“企业级可观测性”**有着极高的前瞻性构思，但当前的实际代码交付却处于**极度精简的“最小可用原型（MVP）”**状态。

以下为发现的 5 大严重实现盲区（代码基本处于留空或未建置状态）：

## 1. 认证安全层全面缺失
**对应文档**：`Security-Authentication.md`
- **设计愿景**：采用高安全级别的 JWT ES256 签名，引入 Redis 双重机制（jti 黑名单防重放）、限制 `aud` (Audience)、以及 Access/Gateway 分层多级 Token。
- **代码现状**：`internal/security/auth.go` 现在的 `Authenticator` 功能**形同虚设**，仅仅暴露了一个硬编码比对 `X-API-Key` 的逻辑。代码中没有任何关于 JWT 解析、黑名单防重放、密码学校验的支持。

## 2. SSRF (服务端请求伪造) 不设防
**对应文档**：`SSRF-Protection.md`
- **设计愿景**：如果后端的 Claude 或基于 Agent 的工具进行 Web 抓取（如 WebFetch 工具），网关必须在协议层阻断内网探测，屏蔽所有 `127.x`、`10.x`、`169.254.x` (云环境元数据劫持) 等 IP，以及防御高阶的 DNS 层重绑定攻击。
- **代码现状**：**物理文件缺失**。`internal/security/` 目录下并没有 `ssrf.go` 拦截器，现阶段网关是一个可以随意代理任何危险请求的裸奔状态。

## 3. 输出炸弹未隔离 (OutputLimiter 遗漏)
**对应文档**：`Resource-Management.md` (§3.1 输出限制)
- **设计愿景**：为了防止模型大杀器（或陷入无限生成死循环）把宿主机内存打量撑爆，需要设置 `MaxLineBytes = 10MB` 和本轮生成的 `MaxTurnBytes = 20MB` 硬性阀门 (`OutputLimiter`)。
- **代码现状**：目前在 `conn.go` 和 `pool` 管理器中并未插入任何对于接收从 Worker 回传流的累加体积统计卡口。

## 4. 可观测性 (Telemetry) 近乎裸奔
**对应文档**：`Observability-Design.md`
- **设计愿景**：接入 `zerolog` 将日志 JSON 结构化；搭建 `/metrics` 端点通过 `Prometheus` 标准导出核心业务监控（如 RED + USE 双方法）；注入 `OpenTelemetry` 实现 Tracing 级联。
- **代码现状**：**物理套件缺失**。项目中不存在 `internal/telemetry` 目录，目前通篇代码仅仅简单调用了系统的 `slog` 向标准输出扔字符串，无法对接任何现代化的云监控体系。

## 5. Event Sourcing（消息持久化）组件悬空
**对应文档**：`Message-Persistence.md`
- **设计愿景**：引入 Append-only 思想构建轻量的 `EventStore` 数据库插件，除了现有的会话属性，还需要独立把每一次 AEP 事件 (`message.delta`, `tool_call` 等) 按时序原子入库以方便独立审计追逆。
- **代码现状**：当前 DB 唯一动作是在 `session/manager.go` 创建 `sessions` 表，文档里提及的 `events` 表与审计追加逻辑均不存在。

---

> [!CAUTION]
> **结论建议**
> 由于第一和第二项（**假认证** 和 **零防护 SSRF**）在此项目代理大模型的性质下具有毁灭性的安全风险（任意内网穿透 / AWS 提权），我强烈建议：如果项目即将推向生产，请**立即下达指令执行这两项功能的代码补齐**。
>
> 至于后三项（输出流控、埋点监控、历史审计库），如果研发精力受限，可以暂时容忍并降级作为 `v1.1` 或后续版本的技术债延后处理。您希望我马上为您针对其中哪些模块制定 Implementation Plan 并开始补洞？
