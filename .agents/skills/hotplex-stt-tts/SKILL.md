---
name: hotplex-stt-tts
description: "初始化或修复 HotPlex 本地 STT 和 MOSS TTS 运行时，包括 Python 依赖、官方模型、配置和验收。仅在明确授权主机变更时使用；只读检查请使用 hotplex-cli 或 hotplex-diagnostics。"
---

# HotPlex STT/TTS

当 HotPlex 报告缺少 `funasr_onnx`、MOSS 依赖/模型，或用户明确要求初始化、修复本地语音运行时时使用本 Skill。

这是 operator 流程。安装包、下载模型、修改 `.env`/config 和重启服务都属于变更；只有用户明确授权对应范围时才能执行。不要暴露凭据、完整环境文件或私有模型/cache 元数据。

详细流程见 [references/initialization.md](references/initialization.md)，修改主机前必须先读取。

## 范围

- STT：HotPlex `local` 或 `feishu+local`，使用 `funasr-onnx` 和 SenseVoiceSmall。
- TTS：HotPlex `moss` 或 `edge+moss`，使用 MOSS-TTS-Nano ONNX sidecar。
- 验收：依赖导入、模型资产、MOSS warmup、`hotplex doctor`、Gateway 健康度和已配置消息渠道连接。

不要用本 Skill 修改语音功能代码、任意清理 Python 环境、泛化选择模型，或在没有初始化/修复请求时进行只读诊断。

## 操作规则

1. 变更前检查已安装 CLI 帮助和当前配置。保留现有 provider、凭据、用户文件和无关环境变量。
2. 优先在当前 HotPlex home 下使用专用 venv。HotPlex checker 和 MOSS 进程通过 `PATH` 解析 `python3`；必须验证同一个解释器能看到全部依赖。不要假设指向 venv 解释器的软链接能保留 venv 识别。
3. 模型使用 reference 列出的官方 ModelScope 仓库；sidecar 脚本使用官方 MOSS 源码仓库。不要使用管道式 shell 安装器、未经审核的镜像或任意模型文件。
4. 只做最小配置变更。重启前先验证配置；已安装服务使用原子命令 `hotplex service restart`，不要拆成手工 stop/start。
5. 安装成功不等于任务完成：执行验收清单，并分别报告通过、警告、失败和有意未修改的状态。

## 完成标准

只有所有请求的运行时检查通过，或存在已记录的外部阻塞时，任务才可结束。至少验证：

- HotPlex 实际调用的解释器可以导入 `funasr_onnx`、`onnxruntime` 和 `onnx`。
- SenseVoiceSmall 资产存在，或已配置 cache 可以解析它们。
- MOSS TTS 和 codec ONNX 资产完整，localhost 冒烟测试中 `/api/warmup-status` 达到 `ready`。
- `hotplex doctor --json` 没有与请求相关的 STT/TTS/AgentConfig failure。
- 任何重启后 Gateway 和用户请求的消息渠道仍然健康。
