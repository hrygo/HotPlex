# Security Package

## OVERVIEW
Security validation layer: API auth, SSRF protection, safe path resolution, env isolation, command/tool/model allowlists, and rate/size limiting.

## STRUCTURE

| File | Purpose |
|------|---------|
| `auth.go` | Authenticator with API key validation + Bot ID extraction |
| `ssrf.go` | URL validation against loopback/private/link-local CIDRs |
| `path.go` | SafePathJoin 5-step validation, dangerous char detection |
| `env.go` | Env var whitelists, sensitive prefix detection, nested agent stripping |
| `env_builder.go` | Worker env construction with whitelist enforcement |
| `command.go` | Binary allowlist: only "claude", "opencode" |
| `tool.go` | Tool category allowlists (Safe/Risky/Network/System) |
| `model.go` | Allowed models whitelist |
| `limits.go` | Rate limiting, MaxEnvelopeBytes = 1MB |

## WHERE TO LOOK

| Task | Location |
|------|----------|
| API key / Bot ID auth | `auth.go` — Authenticator, BotIDFromRequest |
| URL SSRF check | `ssrf.go` — ValidateURL |
| Safe file path | `path.go` — SafePathJoin |
| Env var isolation | `env.go` + `env_builder.go` |
| Binary restriction | `command.go` — AllowedCommands |
| Tool policy | `tool.go` — AllowedTools by category |
| Model restriction | `model.go` — AllowedModels |
| Request limits | `limits.go` — RateLimiter, MaxEnvelopeBytes |

## KEY PATTERNS

**Bot ID transport**: Client sends `X-Bot-ID` header or `bot_id` query param; server extracts via `BotIDFromRequest(r)`. Cross-bot access is rejected at session level.

**SSRF protection**: Protocol check → hostname blocklist → IP prefix check → DNS resolution → blocked CIDRs (loopback 127.0.0.0/8, private 10/8 172.16/12 192.168/16, link-local 169.254.0.0/16 including AWS metadata 169.254.169.254, IPv6)

**SafePathJoin 5-step**: Clean → reject absolute → filepath.Join → filepath.EvalSymlinks → prefix verify (must start with configured root)

**Sensitive env detection**: Prefix matching AWS_, AZURE_, GITHUB_, ANTHROPICIC_, OPENAI_, etc.

**Constant-time comparison**: crypto/subtle.ConstantTimeCompare for API keys and secrets

## ANTI-PATTERNS

- math/rand for token generation — must use crypto/rand
- Shell execution — only claude/opencode binaries, no shell interpreters
- Path separators in command names — "claude", "opencode" only, no "../opencode"
- Cross-bot session access — bot_id must match session owner exactly
- Processing env vars without stripping nested agent configs
- Bypassing SSRF checks for "internal" hostnames without IP validation
