# Secret Management

Secure secret management for HotPlex.

## Usage

```go
import "github.com/hrygo/hotplex/internal/secrets"

// Create manager with environment provider
manager := secrets.NewManager(
    secrets.WithTTL(5 * time.Minute),
)
manager.AddProvider(secrets.NewEnvProvider())

// Get secret
token, err := manager.Get(ctx, "SLACK_BOT_TOKEN")

// Set secret (for current process)
err = manager.Set(ctx, "MY_SECRET", "value")

// Clear cache (e.g., after secret rotation)
manager.ClearCache()
```

## Providers

### EnvProvider
Uses environment variables. Default provider.

### FileProvider (TODO)
Encrypted file-based storage.

### VaultProvider (TODO)
HashiCorp Vault integration.

### AWSSecretsProvider (TODO)
AWS Secrets Manager integration.

## Security Notes

- Secrets are cached in memory with TTL
- Cache is cleared on secret rotation
- Multiple providers can be chained
- First provider wins (priority order)

## Migration from .env

1. Keep .env for local development
2. Use secrets manager in production
3. Rotate secrets regularly
