# README 全面改版设计

**日期：** 2026-07-29  
**范围：** `README.md`、`README_zh.md`

## 目标

把项目首页从技术能力清单改造成面向首次了解 HotPlex 用户的产品入口。读者应能快速理解 HotPlex 解决的问题、判断是否适合自己的场景，并在最短路径内完成首次启动。

## 目标读者

首次接触 HotPlex、正在评估以下需求的开发者或团队：

- 希望把不同 AI Coding Agent 接入统一网关；
- 希望从 Web、Slack 或飞书使用同一套 Agent 能力；
- 希望自托管并掌握会话、安全、审计和运维边界；
- 希望快速验证产品，而不是先阅读完整架构和配置参考。

## 信息架构

README 按用户决策路径组织：

1. **首屏定位**：一句话说明产品价值，列出支持的平台与 Agent，并提供快速开始和完整文档入口。
2. **为什么使用 HotPlex**：用用户收益描述统一接入、多端触达、连续会话和自托管运维能力。
3. **典型使用场景**：团队聊天中的编码助手、远程开发、自动化巡检与定时任务、企业内部自托管接入。
4. **三步快速入门**：安装、运行 `hotplex onboard`、启动网关并访问 Web Chat。
5. **工作方式**：保留现有架构图，用简短文字解释 Client → Gateway → Worker 数据流。
6. **集成矩阵**：集中展示 Web/Slack/飞书和 Claude Code/OpenCode Server/Codex CLI/ACP。
7. **当前版本亮点**：简述 v1.38.1 的繁忙会话连续追问、AEP canonical schema 与跨 SDK 一致性、完整管理后台。
8. **深入资料**：链接安装、配置、部署、协议、SDK、贡献和安全文档。

## 内容取舍

- 首页语言以产品价值和可验证场景为主，避免把内部包名、实现类名和过多配置项放在首屏。
- 保留能建立技术可信度的核心事实：AEP v1、四类 Worker、三类前端、SQLite/PostgreSQL、可观测性和安全边界。
- 删除现有大段配置表；配置默认值统一交给 `docs/reference/configuration.md` 维护。
- SDK 示例保留为更短的入口说明，完整用法下沉到各 SDK 目录。
- 英文与中文 README 采用相同章节、事实和链接，仅做自然语言本地化。
- 修正当前事实偏差：顶层 CLI 为 14 个子命令，`doctor` 注册 27 项检查。

## 事实来源

- 当前版本与版本亮点：`CHANGELOG.md`、`cmd/hotplex/main.go`。
- CLI、安装与启动方式：`cmd/hotplex/`、`INSTALL.md`、`docs/reference/cli.md`。
- 平台与 Worker：`internal/messaging/`、`internal/worker/`。
- AEP schema 与 SDK 一致性：`pkg/aep/schema/`、`docs/reference/aep-protocol.md`。
- 配置与服务地址：`configs/config.yaml`、`docs/reference/configuration.md`。

## 验收标准

- 新用户在首屏和前两个章节内能回答“是什么、为谁服务、解决什么问题”。
- 快速入门提供一条完整且可执行的安装到访问路径。
- 两份 README 章节一一对应，产品事实与命令保持一致。
- 所有相对链接和图片路径存在。
- `make docs-build` 通过，不引入文档断链。
- Markdown 格式检查和针对 README 的链接检查通过。

## 非目标

- 不改变代码、配置默认值、协议或产品行为。
- 不重写文档中心已有的配置、部署、API 和协议参考。
- 不新增截图、演示视频或品牌资产；继续使用现有架构图。
