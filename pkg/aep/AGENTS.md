# pkg/aep — AEP v1 编解码器

## OVERVIEW
AEP v1 协议 codec。把 `events.Envelope` 序列化成 NDJSON 一行、反序列化回结构体，并执行字段校验。Gateway、所有客户端 SDK、Worker 适配器共用此包，没有第二个真相来源。

## STRUCTURE
```
codec.go     # 全部公开 API：Encode/Decode/Validate/ID 生成/Envelope 工厂 + 内部 NDJSON 安全处理
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| 流式编码（带 `\n`） | `codec.go:27` | `Encode(w io.Writer, env *Envelope) error` |
| 流式解码 | `codec.go:97` | `Decode(r io.Reader) (*Envelope, error)`，`DisallowUnknownFields` |
| 单行编码（无 `\n`） | `codec.go:201` | `EncodeJSON(env) ([]byte, error)` |
| 单行解码（完整校验） | `codec.go:128` | `DecodeLine([]byte) (*Envelope, error)` |
| 客户端→服务端宽松解码 | `codec.go:116` | `DecodeLineMinimal`：不要求 session_id/seq（Gateway 后补） |
| 完整字段校验 | `codec.go:140` | `Validate`：version/id/session_id/seq>0/timestamp>0/event.type |
| 宽松校验 | `codec.go:178` | `ValidateMinimal`：仅 version 一致性 + event.type 非空 |
| Envelope 工厂 | `codec.go:243/250` | `NewInputEnvelope`、`NewPingEnvelope`（Ping seq=0 + PriorityControl） |
| ID 生成 | `codec.go:189/194` | `NewID()` → `evt_<uuid>`，`NewSessionID()` → `sess_<uuid>` |
| 时间注入 | `codec.go:38-48/271` | `marshalEnvelope` 在浅拷贝上盖 `events.Version` 和 timestamp=0 时补当前 ms；入参不被改写 |

## 函数签名（codec.go）
```go
Encode(w io.Writer, env *events.Envelope) error                          // :27
Decode(r io.Reader) (*events.Envelope, error)                            // :97
EncodeJSON(env *events.Envelope) ([]byte, error)                         // :201
MustMarshal(env *events.Envelope) []byte                                 // :206 panic on err
DecodeLine(data []byte) (*events.Envelope, error)                        // :128
DecodeLineMinimal(data []byte) (*events.Envelope, error)                 // :116
Validate(env *events.Envelope) error                                     // :140
ValidateMinimal(env *events.Envelope) error                              // :178
NewID() string                                                           // :189 "evt_"+uuid
NewSessionID() string                                                    // :194 "sess_"+uuid
NewInputEnvelope(sessionID, content string) *events.Envelope             // :243
NewPingEnvelope(sessionID string) *events.Envelope                       // :250
IsSessionBusy(env *events.Envelope) bool                                 // :215
IsTerminalEvent(kind events.Kind) bool                                   // :228 (Done|Error)
ParseSessionID(id string) string                                         // :233 trim "sess_"
SeqKey(sessionID, eventID string) string                                 // :238 去重键
EscapeJSTerminators(data []byte) []byte                                  // :92
```

## 版本控制
**单一协议版本字段**：`events.Version = "aep/v1"`（events.go:9）。没有 `aep/v2`、没有 per-event version。`Validate`（codec.go:145）严格 `env.Version != events.Version` 即报 `unsupported version`，客户端发空 version 通过 `ValidateMinimal` 放行（Gateway 在 init 阶段补盖）。

## KEY PATTERNS
**NDJSON 安全（codec.go:22/62）** — `escapeJSTerminators` 把 U+2028/U+2029 转义成 `\u2028`/`\u2029`。Go `json.Marshal` 对 string 已自动转义，此函数兜底处理 `map[string]any` 里 raw []byte 的边界情况。任何把 NDJSON 行当 JS 字符串求值的消费者都会在裸 codepoint 处静默截断。

**入参零改写（codec.go:38-48）** — `marshalEnvelope` 做 shallow copy 后填 Version/Timestamp 再 marshal，原 `*Envelope` 字段不动。这是 `Clone` + EncodeJSON 在并发发送链路能安全共存的根因。

**三档校验强度** — `Validate`（全字段，Gateway 内部 / Worker 出口）> `ValidateMinimal`（C→S 入口宽松）> 无校验（`MustMarshal` 调用方自负）。`Decode` 还额外开 `DisallowUnknownFields` 拒绝未知字段，防 schema drift。

**错误包装** — 所有 decode/validate 失败统一 `fmt.Errorf("aep: <op> envelope: %w", err)`，调用方 `errors.Is/As` 解包。

## ANTI-PATTERNS
- ❌ 用 `json.Marshal(env)` 直接发线，绕过 `Encode/EncodeJSON`（丢 NDJSON 转义 + 版本/时间戳注入）
- ❌ 依赖 `nowFunc` 包变量做生产逻辑（仅测试 mock 用，默认 wall-clock）
- ❌ 在客户端代码里手搓 init 握手（pkg/README.md 提到的 `NewInitEnvelope`/`ValidateInit`/`BuildInitAck` 在本包**不存在**，属 internal/aep 范畴）
- ❌ 复用 decoded 出来的 `*Envelope` 跨 goroutine 而不 Clone（Event.Data 是 `interface{}`，map 共享会 race）

## PUBLIC API STABILITY
被 `client/`、`examples/{typescript,python,java}-client/`、Gateway、所有 Worker 适配器直接调用。函数签名和 NDJSON wire 格式是冻结契约，**禁止改签名、禁止改 `DisallowUnknownFields` 行为、禁止改 `"evt_"/"sess_"` 前缀**。新增可选字段走 `ValidateMinimal` 宽松路径。破坏性变更需 major 版本 bump，且需同步更新所有 SDK。
