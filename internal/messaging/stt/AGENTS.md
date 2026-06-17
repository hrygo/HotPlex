# STT Package

## OVERVIEW
后端无关的语音转文字模块。定义 `Transcriber` 接口，提供本地命令、持久子进程、Fallback 三种实现。实际模型（Whisper/SenseVoice 等）由外部 `cmdTemplate` 决定，本包只管子进程生命周期与 JSON-over-stdio 协议。

## STRUCTURE
```
stt/
  stt.go              # Transcriber/Closer 接口 + LocalSTT/FallbackSTT/PersistentSTT + 工具函数
  stt_job_unix.go     # closeJob no-op（Unix 用 PGID 清理）
  stt_job_windows.go  # closeJob 调 proc.CloseJobHandle（Job Object KILL_ON_JOB_CLOSE）
  *_test.go           # 测试
```

## WHERE TO LOOK
| 任务 | 位置 | 说明 |
|------|------|------|
| Transcriber 接口 | `stt.go:27` | `Transcribe(ctx, audio) (string, error)` + `RequiresDisk() bool` |
| 本地命令实现 | `stt.go:47` | `LocalSTT`: `{file}` 占位符，stdout 首行即结果 |
| Fallback 实现 | `stt.go:86` | primary 失败或空 → secondary |
| 持久子进程实现 | `stt.go:130` | `PersistentSTT`: JSON-over-stdio，模型常驻内存 |
| stdin/stdout 协议 | `stt.go:199` | 入 `{"audio_path":...}`，出 `{"text":...,"error":...}` |
| 双锁设计 | `stt.go:136-138` | `mu`（生命周期）+ `ioMu`（串行化 I/O）+ `inFlight` |
| 可中断读行 | `stt.go:234` | `readLine`: ctx.Done 时 kill 子进程解锁 reader |
| 分层终止 | `stt.go:364` | close stdin → GracefulTerminate PGID → 5s → ForceKill |
| 空闲关闭 | `stt.go:450` | `idleMonitor`: 30s tick，inFlight + lastUsed vs idleTTL |
| 临时音频文件 | `stt.go:488` | `<TempBaseDir>/media/stt_tmp/stt_<nanos>.opus`，defer 删除 |
| Opus → PCM | `stt.go:503` | `AudioToPCM`: ffmpeg pipe → s16le 16kHz mono（供云端 API） |
| 引用计数共享 | `stt.go:549` | `SharedTranscriber`: 多适配器共享同一子进程 |
| 平台分离点 | `stt_job_*.go` | 仅 `closeJob`：Unix no-op，Windows 关 Job Object 句柄 |

## KEY PATTERNS

**后端无关设计**
- 本包**不绑定**任何具体模型。Whisper / SenseVoice / 任何 CLI 都通过 `cmdTemplate` 注入
- `PersistentSTT` 只负责：spawn 子进程、喂 audio_path、收 JSON、管生命周期
- 配置示例：`stt.command: "sensevoice-server --port 0"`（外部二进制）

**PersistentSTT 协议契约**
```
Go → stdin:  {"audio_path": "/tmp/.../stt_123.opus"}\n
子进程 → stdout: {"text": "转录结果", "error": ""}\n
```
- stdout 行缓冲 64KB 起，上限 10MB（`stt.go:332`）
- 读超时默认 30s（`readTimeout`），caller ctx 无 deadline 时兜底

**进程隔离（跨平台）**
- `proc.SetSysProcAttr`: Unix 设 PGID，Windows 设 CREATE_NEW_PROCESS_GROUP
- `proc.CreateAndAssignJob`: Windows 绑 Job Object，Unix 返回空句柄
- `proc.TrackPID`: 写全局 PID 文件，gateway 崩溃后启动期清理孤儿

**idleMonitor 防死锁**
- 不在 `terminate` 内等 `<-done`：idleMonitor 可能阻塞在调用方持有的 `mu` 上
- 改为 cancel ctx 后让 monitor 自行退出（`stt.go:354` 注释）

## ANTI-PATTERNS
- ❌ 在本包硬编码模型名 —— 模型由 `cmdTemplate` 配置，保持后端无关
- ❌ `readLine` 不设超时 —— 子进程卡住会永久阻塞，必须用 ctx 或 `readTimeout`
- ❌ 临时音频文件不 defer 删除 —— 会撑爆 `<TempBaseDir>/media/stt_tmp`
- ❌ Windows 跳过 `closeJob` —— 仅 PGID kill 清不掉子进程树，必须关 Job Object
