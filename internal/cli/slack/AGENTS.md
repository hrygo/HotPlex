# slack — Slack API Client Wrappers for `hotplex slack`

## OVERVIEW
Thin, context-aware wrappers around `github.com/slack-go/slack` used by the `hotplex slack` cobra subcommands (which live in `cmd/hotplex/slack_*.go`). Package name is `slackcli` to avoid collision with the upstream library. All functions take a pre-built `*slack.Client`; config/env resolution happens in `LoadConfigAndClient`.

## STRUCTURE
```
slack/
  client.go      # NewClient, ResolveChannel/ResolveThreadTS, LoadConfigAndClient, loadEnvFile
  message.go     # SendMessage, UpdateMessage, ScheduleMessage  (+ SendResult)
  channels.go    # ListChannels (paginated, types filter)        (+ ChannelInfo)
  bookmark.go    # AddBookmark, ListBookmarks, RemoveBookmark    (+ BookmarkResult)
  upload.go      # UploadFile with MaxSize guard                 (+ UploadParams/UploadResult)
  download.go    # DownloadFile (MkdirAll + cleanup on failure)
  delete.go      # DeleteFile
  react.go       # AddReaction, RemoveReaction
  client_test.go # NewClient + env-load tests
  delete_test.go # DeleteFile validation
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Build a client from config | `client.go:40` | `LoadConfigAndClient`: resolve path → load `.env` → `config.Load` → verify `Slack.Enabled` → `NewClient` |
| Env file loader | `client.go:67` | `loadEnvFile` skips protected keys via `security.IsProtected`, never overwrites existing env values |
| Channel resolution | `client.go:23` | `ResolveChannel`: `--channel` flag → `HOTPLEX_SLACK_CHANNEL_ID` env → error |
| Send / update / schedule | `message.go:16` / `:29` / `:37` | All accept `threadTS`; `ScheduleMessage` takes unix `postAt` int64 |
| List with pagination | `channels.go:33` | Loops `GetConversationsContext` cursor until `limit` reached |
| Bookmark CRUD | `bookmark.go:17` / `:38` / `:57` | Add always sets `Type: "link"` |
| Upload size guard | `upload.go:34` | `UploadParams.MaxSize > 0` rejects oversize before upload |
| Download cleanup | `download.go:18` | `MkdirAll` on out dir; on download error closes fd and `os.Remove`s partial file |
| Reaction add/remove | `react.go:10` / `:21` | Build `slack.ItemRef{Channel, Timestamp}` then call context variant |

## KEY PATTERNS

**Verb → file mapping** (cobra commands in `cmd/hotplex/slack_*.go`)
- `send-message` → `SendMessage`, `update-message` → `UpdateMessage`, `schedule-message` → `ScheduleMessage`
- `upload-file` → `UploadFile`, `download-file` → `DownloadFile`, `delete-file` → `DeleteFile` (also `--file-id` validation)
- `list-channels` → `ListChannels` (supports `--types im,public_channel`)
- `bookmark add` / `bookmark list` / `bookmark remove` → bookmark.go
- `react add` / `react remove` → react.go

**Consistent error wrapping**
- Every wrapper returns `fmt.Errorf("<verb>: %w", err)` so the cobra layer can surface a single causal chain.

**Result structs are JSON-tagged**
- `SendResult`, `BookmarkResult`, `UploadResult`, `ChannelInfo` all carry `json:"..."` tags so the CLI can render `--output json` without re-marshaling.

**Env hygiene**
- `loadEnvFile` only `os.Setenv` when the key is unset AND not `security.IsProtected` — secrets in `.env` never leak into child processes or override explicit env.

## ANTI-PATTERNS
- ❌ Construct `*slack.Client` inline in a command — go through `LoadConfigAndClient` so `.env` + `Slack.Enabled` checks always run.
- ❌ Drop the `context.Context` parameter — every API call must use the `*Context` variant for cancellation/timeout.
- ❌ Leave partial downloads on failure — `DownloadFile` already removes them; new code paths must follow suit.
- ❌ Add a slack verb without a JSON-tagged result struct — breaks `--output json` consumers.
