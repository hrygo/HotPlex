# Self-Diagnostics System - Implementation Guide

## Overview

The self-diagnostics system (Issue #219) provides automatic error capture,and analysis for HotPlex. When errors occur, it automatically collects context, analyzes the problem using LLM, and can create GitHub issues.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│   Trigger   │────▶│ Diagnostician│────▶│   Notifier   │
│ (auto/cmd)  │     │   (core)      │     │  (channel)   │
└─────────────┘     └──────────────┘     └──────────────┘
       │                   │                    │
       ▼                   ▼                    ▼
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  Collector  │     │IssueCreator  │     │   Cache      │
│ (context)   │     │  (gh CLI)    │     │  (dedup)     │
└─────────────┘     └──────────────┘     └──────────────┘
```

## Components

### 1. Diagnostician (core)

Location: `internal/diag/diagnostician.go`

The core engine that orchestrates the diagnostic workflow:
- Collects context from session, logs, and environment
- Generates issue previews using Brain (LLM)
- Manages pending diagnoses with timeout handling

```go
// Create a new diagnostician
diag := diag.NewDiagnostician(config, historyStore, brain, logger)

// Run diagnosis
result, err := diag.Diagnose(ctx, trigger)

// Confirm issue creation
issueURL, err := diag.ConfirmIssue(ctx, result.ID)

// Ignore a diagnosis
err := diag.IgnoreIssue(ctx, result.ID)
```

### 2. Collector

Location: `internal/diag/collector.go`

Gathers diagnostic context:
- Session metadata (ID, platform, user)
- Conversation history (with auto-summarization for large conversations)
- Environment info (version, OS, uptime)
- Recent logs

```go
collector := diag.NewCollector(config, historyStore, brain)
ctx, err := collector.Collect(ctx, trigger)
```

### 3. Redactor

Location: `internal/diag/redact.go`

Sanitizes sensitive data before including in diagnostics:
- API keys (various formats)
- Slack tokens (xoxb, xoxp, etc.)
- GitHub tokens (ghp_, gho_, ghu_)
- AWS credentials (AKIA...)
- Private keys
- JWT tokens
- Connection strings
- Email addresses

```go
redactor := diag.NewRedactor(diag.RedactStandard)
sanitized := redactor.Redact(input)
```

### 4. Issue Creator

Location: `internal/diag/issue.go`

Creates GitHub issues via:
1. `gh` CLI (preferred)
2. GitHub API (fallback)

```go
creator := diag.NewIssueCreator(config, logger)
result, err := creator.Create(ctx, diagResult)
```

### 5. Notifier

Location: `internal/diag/notifier.go`

Sends diagnostic notifications to fixed channels:
- Supports multiple platforms (Slack, Discord, etc.)
- Includes action buttons for user confirmation
- Falls back to auto-creating issues on timeout

```go
notifier := diag.NewNotifier(config, diagnostician, logger)
notifier.RegisterAdapter("slack", adapter, channelID)
notifier.Notify(ctx, result)
```

### 6. Cache

Location: `internal/diag/cache.go`

Deduplicates similar diagnoses using content hashing:
- TTL-based expiration
- LRU eviction when full
- Hash based on session ID, error type, and conversation content

```go
cache := diag.NewDiagnosisCache(30*time.Minute, 100)
dupResult := cache.CheckDuplicate(diagCtx)
if dupResult.IsDuplicate {
    // Use existing diagnosis
}
```

### 7. Metrics

Location: `internal/diag/metrics.go`

Collects operational metrics:
- Diagnosis counts (total, success, failed)
- Issue creation counts
- Cache hit/miss rates
- Average diagnosis duration

```go
metrics := diag.NewMetrics()
snapshot := metrics.Snapshot()
```

### 8. /diagnose Command

Location: `chatapps/command/diagnose_executor.go`

Slash command for manual diagnosis:
- Only works in configured diagnostics channel
- Creates issue directly (no confirmation needed)
- Shows progress with emitter

```go
executor := command.NewDiagnoseExecutor(diagnostician, config, logger)
registry.Register(executor)
```

## Configuration

```yaml
diagnostics:
  enabled: true
  notify_channel: "C12345678"        # Fixed notification channel
  log_size_limit: 20480              # 20KB
  conversation_size_limit: 20480     # 20KB
  confirm_timeout: 5m                # 5 minutes
  github:
    repo: "hrygo/hotplex"
    labels: ["bug", "auto-diagnosed"]
```

## Trigger Types

| Type | Description | Issue Creation |
|------|-------------|----------------|
| `auto` | Triggered by error hooks | Requires confirmation (5min timeout) |
| `command` | Triggered by /diagnose | Direct creation |

## Error Types

| Type | Description |
|------|-------------|
| `exit` | CLI process exited abnormally |
| `timeout` | Session timeout |
| `waf_violation` | Security/WAF violation |
| `panic` | Panic or crash |
| `unknown` | Unclassified error |

## Data Structures

### DiagContext

```go
type DiagContext struct {
    OriginalSessionID string
    Platform          string
    UserID            string
    ChannelID         string
    ThreadID          string
    Trigger           DiagTrigger
    Error             *ErrorInfo
    Conversation      *ConversationData
    Logs              []byte
    Environment       *EnvInfo
    Timestamp         time.Time
}
```

### DiagResult

```go
type DiagResult struct {
    ID          string
    Context     *DiagContext
    Preview     *IssuePreview
    IssueURL    string
    IssueNumber int
    Status      DiagStatus
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### IssuePreview

```go
type IssuePreview struct {
    Title         string
    Labels        []string
    Priority      string
    Summary       string
    Reproduction  string
    Expected      string
    Actual        string
    RootCause     string
    SuggestedFix  string
}
```

## Workflow

### Auto-Diagnosis Flow

```
1. Error occurs (exit/timeout/WAF)
2. Diagnostician.Diagnose() called with TriggerAuto
3. Collector gathers context
4. Brain generates IssuePreview
5. Result stored as pending with 5min timer
6. Notifier sends to fixed channel with buttons
7a. User clicks "Create" → Issue created
7b. User clicks "Ignore" → Status = ignored
7c. 5min timeout → Auto-create issue
```

### Manual Diagnosis Flow

```
1. User runs /diagnose in diagnostics channel
2. DiagnoseExecutor.Execute() called
3. Diagnostician.Diagnose() with TriggerCommand
4. IssueCreator.Create() directly
5. Success message with issue URL
```

## Security Considerations

1. **Sensitive Data Redaction**: All content is redacted before inclusion
2. **Channel Restriction**: /diagnose only works in configured channel
3. **Token Protection**: GitHub tokens from secrets provider
4. **No Panics**: All errors are returned, never panic

## Integration Points

### 1. Engine Error Hooks

```go
// In engine/runner.go
func (r *Runner) handleError(sessionID string, err error) {
    trigger := diag.NewBaseTrigger(diag.TriggerAuto, sessionID, errInfo)
    r.diagnostician.Diagnose(ctx, trigger)
}
```

### 2. Slash Command Registration

```go
// In chatapps/setup.go
diagExecutor := command.NewDiagnoseExecutor(diagnostician, config, logger)
registry.Register(diagExecutor)
```

### 3. Storage Integration

```go
// Using plugins/storage for message history
historyStore := persistence.NewStorageBackedHistory(storagePlugin)
```

## Testing

### Unit Tests

```bash
go test ./internal/diag/... -v
```

### Key Test Cases

- `TestDefaultConfig` - Configuration defaults
- `TestNewBaseTrigger` - Trigger builder
- `TestRedactAPIKey` - API key redaction
- `TestRedactSlackToken` - Slack token redaction
- `TestDiagnosisCache` - Cache deduplication

## Monitoring

### Metrics Available

```go
snapshot := diag.GetGlobalMetrics().Snapshot()
// snapshot.DiagnosesTotal
// snapshot.DiagnosesSuccess
// snapshot.IssuesCreated
// snapshot.CacheHitRate
// snapshot.AvgDiagnosisMs
```

### Prometheus Integration

```go
// Expose metrics
prometheus.MustRegister(promauto.NewGaugeFunc(
    prometheus.GaugeOpts{
        Name: "hotplex_diag_pending",
        Help: "Pending diagnoses",
    },
    func() float64 { return float64(snapshot.PendingDiagnoses) },
))
```

## Future Enhancements

1. **Webhook Integration**: POST diagnostics to external services
2. **Duplicate Detection**: ML-based similarity matching
3. **Auto-Resolution**: Suggest and apply fixes automatically
4. **Dashboard**: Real-time diagnostics monitoring

## Troubleshooting

### Issue: Diagnoses not being created

1. Check `diagnostics.enabled` is true
2. Verify `GITHUB_TOKEN` is set or `gh` CLI is authenticated
3. Check logs for `diag` component errors

### Issue: /diagnose command rejected

1. Verify command is run in `notify_channel`
2. Check user has permission to run diagnostics

### Issue: GitHub issue creation fails

1. Run `gh auth status` to verify CLI auth
2. Check `GITHUB_TOKEN` has `repo` scope
3. Verify `github.repo` configuration is correct
