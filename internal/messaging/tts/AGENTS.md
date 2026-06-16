# TTS Package

## OVERVIEW
Edge-TTS + MOSS 双引擎语音合成，FFmpeg 格式转换，LLM 长文摘要。实现 `Synthesizer` 接口，供 Slack/Feishu 适配器共享调用。无平台分离文件，全平台纯 Go。

## STRUCTURE
```
tts/
  tts.go            # Synthesizer/Closer 接口 + FallbackSynthesizer + SharedSynthesizer + 工厂
  edge.go           # Edge-TTS 原生 WebSocket 实现（无三方依赖）
  moss.go           # MossSynthesizer：委托 MossProcess，返回 WAV
  moss_process.go   # MOSS-TTS-Nano FastAPI sidecar 进程管理
  audio.go          # FFmpeg 转换：ToOpus/ToMP3 + Ogg 时长解析
  prompt.go         # LLM 摘要（brain）+ SanitizeForSpeech 文本净化
  truncate.go       # 句末标点感知截断
  *_test.go         # 测试
```

## WHERE TO LOOK
| 任务 | 位置 | 说明 |
|------|------|------|
| Synthesizer 接口 | `tts.go:17` | `Synthesize(ctx, text) ([]byte, error)` |
| 默认引擎装配 | `tts.go:123` | `NewConfiguredSynthesizer`: Edge 主 + MOSS 备 Fallback |
| Edge 令牌算法 | `edge.go:36` | `generateSecMSGec`: 5min 取整 → Windows 100ns ticks → SHA256 |
| Edge WebSocket 握手 | `edge.go:66` | `synthesizeEdge`: config msg → SSML msg → 收 `turn.end` |
| SSML 模板 | `edge.go:154` | `<speak><voice><prosody pitch rate volume>` |
| SSML 转义 | `edge.go:180` | `sanitizeSSMLText`: XML 实体 + 剔除控制字符 |
| 音频 wire 解析 | `edge.go:129` | 2 字节大端头长 + headers + audio |
| FFmpeg → Opus（Feishu） | `audio.go:14` | `ToOpus`: 24kHz mono libopus，stdin/stdout pipe |
| FFmpeg → MP3（Slack） | `audio.go:47` | `ToMP3`: 已是 MP3 则跳过（`isMP3` 探测） |
| Ogg 时长 | `audio.go:120` | `ParseOggDurationMs`: 扫页取最大 granule（48kHz） |
| MOSS sidecar 启动 | `moss_process.go:214` | `start`: single-flight + 60s warmup 轮询 `/api/warmup-status` |
| MOSS 合成 API | `moss_process.go:87` | POST `/api/generate` form: text/voice/max_new_frames=150 |
| MOSS 空闲关闭 | `moss_process.go:390` | `idleMonitor`: 30s tick，activeCount + lastUsed vs idleTTL |
| LLM 播报摘要 | `prompt.go:52` | `SummarizeForTTS`: brain.Global + 中文口语化 system prompt |
| 播报文本净化 | `prompt.go:84` | `SanitizeForSpeech`: 去 markdown/零宽字符/裸路径，大数中文化 |

## KEY PATTERNS

**Edge Sec-MS-GEC 令牌（`edge.go`）**
- 时间戳 floor 到 300s 边界 → 转 Windows epoch 100ns ticks → `SHA256(ticks + TrustedClientToken)` 大写 hex
- `TrustedClientToken` 硬编码常量，Chromium UA + Origin 伪装成 Edge 浏览器扩展

**Fallback 链（`tts.go`）**
- `FallbackSynthesizer`: primary 失败 → ctx 未取消才尝试 secondary
- `SharedSynthesizer`: 原子引用计数，多适配器共享同一 MOSS sidecar 进程

**MOSS sidecar 生命周期（`moss_process.go`）**
- 懒启动 + single-flight（`readyCh`）：首调用者 spawn+warmup，余者等通知
- 分层终止：GracefulTerminate PGID → 5s → ForceKill，`proc.TrackPID` 孤儿清理
- 崩溃自愈：`maybeRestart` 检测进程死亡 → 置 `started=false`，下次请求重启

**输出格式约定**
- Edge 输出 MP3（24kHz mono），MOSS 输出 WAV（48kHz stereo）
- 平台发送前转码：Feishu 需 Opus（`ToOpus`），Slack 需 MP3（`ToMP3`）

## ANTI-PATTERNS
- ❌ 直连 Edge 不带 `Sec-MS-GEC` —— 令牌 5 分钟过期，必须现算
- ❌ 跳过 `sanitizeSSMLText` —— 未转义的 `<>&'` 会破坏 SSML 解析
- ❌ MOSS sidecar 退出不调 `Close` —— 会泄漏 python3 进程（靠 PID tracker 兜底）
- ❌ 长文直接送 TTS —— 先 `SummarizeForTTS` 摘要再合成，避免超长音频
