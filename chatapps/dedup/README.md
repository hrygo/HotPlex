# Event Deduplication

This package provides event deduplication to prevent duplicate message processing.

## Usage

```go
import "github.com/hrygo/hotplex/chatapps/dedup"

// Before logging
logger.Info("Config loaded", "token", dedup.RedactSensitiveData(config.BotToken))

// Or wrap your logger
type RedactedLogger struct {
    logger *slog.Logger
}

func (r *RedactedLogger) Info(msg string, args ...any) {
    for i := 1; i < len(args); i += 2 {
        if str, ok := args[i].(string); ok {
            args[i] = dedup.RedactSensitiveData(str)
        }
    }
    r.logger.Info(msg, args...)
}
```

## Supported Patterns

- **Slack tokens**: `xoxb-*`, `xoxp-*`, `xoxa-*`, `xoxr-*`
- **GitHub tokens**: `ghp_*`, `gho_*`, `github_pat_*`
- **API keys**: `sk-*`, `Bearer *`

## Performance

- Single token: ~237 ns/op
- Memory: 144 B/op, 2 allocs/op

## Security Notes

- Always redact before logging
- Never log raw tokens/secrets
- Use environment variables for sensitive config
