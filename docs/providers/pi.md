# Pi Provider (pi-coding-agent)

This document describes the pi Provider integration for HotPlex, a including installation, configuration, and usage examples.

## Overview

Pi is a minimal terminal coding harness developed by Mario Zechner (@badlogic). It supports 15+ LLM providers through a unified API.

Key features:
- Multi-provider support (Anthropic, OpenAI, Google, Mistral, Groq, Cerebras, xAI, OpenRouter, etc.)
- JSON mode for structured output
- RPC mode for process integration
- Session management with JSONL storage
- Interactive coding agent capabilities

## Installation

```bash
# Install globally via npm
npm install -g @mariozechner/pi-coding-agent

# Verify installation
pi --version
```

Or install a specific version:
```bash
npm install -g @mariozechner/pi-coding-agent@<version>
```

## Configuration

### HotPlex Configuration

Add to your `hotplex.yaml` provider configuration:

```yaml
providers:
  pi:
    enabled: true
    type: pi
    # Optional: custom binary path
    # binary_path: /usr/local/bin/pi
    # Optional: default model
    default_model: claude-sonnet-4-20250514
    # Provider-specific options
    pi:
      provider: anthropic    # LLM provider (anthropic, openai, google, etc.)
      model: claude-sonnet-4-20250514  # Model ID or pattern
      thinking: high              # Thinking level: off, minimal, low, medium, high, xhigh
      use_rpc: false            # Use RPC mode for stdin/stdout integration
      session_dir: ""           # Custom session storage directory
      no_session: false         # Ephemeral mode (don't save session)
```

### Environment Variables

Set the following environment variables based on your chosen provider:

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Required for Anthropic |
| `OPENAI_API_KEY` | Required for OpenAI |
| `GOOGLE_API_KEY` | Required for Google |
| `MISTRAL_API_KEY` | Required for Mistral |
| `GROQ_API_KEY` | Required for Groq |
| `CEREBRAS_API_KEY` | Required for Cerebras |
| `XAI_API_KEY` | Required for xAI |
| `OPENROUTER_API_KEY` | Required for OpenRouter |

## Event Types

Pi outputs events in JSON Lines format

### Session Events

```json
{"type":"session","version":3,"id":"uuid","timestamp":"...","cwd":"/path"}
```

### Agent Events
```json
{"type":"agent_start"}
{"type":"agent_end","messages":[...]}
```

### Turn Events
```json
{"type":"turn_start"}
{"type":"turn_end","message":{...},"toolResults":[...]}
```

### Message Events
```json
{"type":"message_start","message":{...}}
{"type":"message_update","message":{...},"assistantMessageEvent":{...}}
{"type":"message_end","message":{...}}
```

### Tool Events
```json
{"type":"tool_execution_start","toolCallId":"id","toolName":"name","args":{...}}
{"type":"tool_execution_end","toolCallId":"id","toolName":"name","result":...,"isError":false}
```

## CLI Arguments

Pi supports the following CLI arguments

| Argument | Description |
|----------|-------------|
| `--mode json` | Output events as JSON lines (required for HotPlex) |
| `--provider <name>` | LLM provider to use |
| `--model <pattern>` | Model ID or pattern (supports provider/id format) |
| `--thinking <level>` | Thinking level |
| `--session <id>` | Resume session |
| `--session-dir <dir>` | Custom session directory |
| `--no-session` | Ephemeral mode |
| `--continue` | Continue most recent session |
| `--resume` | Browse and select session |

## Usage Examples

### Basic Usage

```go
cfg := &config.ProviderConfig{
    Type:    provider.ProviderTypePi,
    Enabled: true,
    Pi: &provider.PiConfig{
        Provider: "anthropic",
        Model:    "claude-sonnet-4-20250514",
    },
}

provider, err := provider.NewPiProvider(cfg, nil)
if err != nil {
    log.Fatal(err)
}

args := provider.BuildCLIArgs("session-id", &provider.ProviderSessionOptions{
    InitialPrompt: "Hello, pi!",
})
fmt.Println(args)
// Output: [--mode json --provider anthropic --model claude-sonnet-4-20250514 Hello, pi!]
```

### With RPC Mode

```go
cfg.Pi.UseRPC = true
args := provider.BuildCLIArgs("", nil)
// Output: [--mode json --no-session]
```

### Event Parsing

```go
events, err := provider.ParseEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`)
for _, event := range events {
    fmt.Printf("Event type: %s, Content: %s\n", event.Type, "answer"
    }
}
```

## Supported Providers

Pi supports the following LLM providers

| Provider | API Key | Models |
|----------|---------|------|
| Anthropic | `ANTHROPIC_API_KEY` | claude-sonnet-4-20250514, claude-opus-4-6, claude-3-5-sonnet-4-20250514 |
| OpenAI | `OPENAI_API_KEY` | gpt-4o, gpt-4.1-turbo, gpt-4o-mini |
| Google | `GOOGLE_API_KEY` | gemini-2.5-pro, gemini-2.0-flash-exp |
| Mistral | `MISTRAL_API_KEY` | mistral-large-latest |
| Groq | `GROQ_API_KEY` | llama-3.3-70b, mixtral-8x7b |
| Cerebras | `CEREBRAS_API_KEY` | llama-3.3-70b |
| xAI | `XAI_API_KEY` | grok-2-latest |
| OpenRouter | `OPENROUTER_API_KEY` | (various models) |

| Kimi | `KIMI_API_KEY` | (various models) |

## Troubleshooting

### Binary Not Found

```bash
# Install pi CLI
npm install -g @mariozechner/pi-coding-agent
```

### Invalid Thinking Level

Check configuration

```yaml
pi:
  thinking: invalid-level  # Error: invalid thinking level: invalid-level
```

### Session Not Resuming
Ensure session ID is valid

```yaml
pi:
  session_dir: /nonexistent/path
```

## Architecture

The Pi Provider follows the same Strategy Pattern as other HotPlex providers

┌─────────────────────────────────────────────────────────────────────────────┐
│                         HotPlex Architecture                                │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐    ┌─────────────┐                                           │
│  │   Slack    │    │  Feishu   │                                           │
│  │  Adapter   │    │   Adapter  │                                           │
│  └──────┬──────┘    └──────┬──────┘                                           │
│         │                   │                   │                              │
│         └───────────────────┴───────────────────┘                              │
│                                       │                                         │
│                                       ▼                                         │
│                        ┌──────────────────────────┐                              │
│                        │   ChatApps EngineHandler │                              │
│                        └────────────┬─────────────┘                              │
│                                     │                                           │
│                                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────────────────────┐  │
│  │                         Provider Layer (Strategy Pattern)                 │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐             │  │
│  │  │ Claude Code   │  │   OpenCode     │  │   pi-mono      │             │  │
│  │  │   Provider    │  │    Provider    │  │   Provider     │             │  │
│  │  │ (claude CLI) │  │ (opencode CLI) │  │ (pi CLI/npm)   │             │  │
│  │  └───────┬────────┘  └───────┬────────┘  └───────┬────────┘    │  │
│  │          │                   │                   │                   │                       │  │
│  │          └───────────────────┴───────────────────┘                       │  │
│  └─────────────────────────────────────────────────────────────────────────┘  │
│                                     │                                             │
│                                 │                                               │
│  ┌─────────────────────────────────────────────────────────────────────────┐  │
│  │                         HotPlex Engine                                   │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐            │  │
│  │  │ Session Pool  │  │   I/O Mux      │  │  Event Parser  │            │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘            │  │
│  └─────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Integration Testing

For integration tests with actual pi CLI, set the environment variable and run:

```bash
# Skip tests that require pi CLI
go test -v -short ./...
```

## Resources

- [pi-mono Repository](https://github.com/badlogic/pi-mono)
- [pi-coding-agent Documentation](https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent)
- [JSON Mode Documentation](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/json.md)
