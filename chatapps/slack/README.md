# Slack Adapter Package

This package implements a high-performance, AI-native Slack adapter for the HotPlex engine. It provides seamless integration with Slack, supporting rich visual components through Block Kit, native streaming via Assistant Threads, and multiple connection modes.

## Core Modules

### 🔌 Connectivity
- **[adapter.go](adapter.go)**: The central entry point. Manages the adapter lifecycle, configuration, and coordinates specialized modules.
- **[socketmode.go](socketmode.go)**: Handles WebSocket-based connections using Slack Socket Mode (preferred).
- **[events.go](events.go)**: Manages HTTP-based Events API callbacks and signature verification.

### 💬 Messaging & UI
- **[messages.go](messages.go)**: Core messaging logic, including standard posts, updates, and native streaming API calls.
- **[builder.go](builder.go)**: A sophisticated factory that translates engine `ChatMessage` objects into Slack Block Kit components.
- **[formatting.go](formatting.go)**: Converts standard Markdown to Slack's `mrkdwn` format, following CommonMark precedence.
- **[chunker.go](chunker.go)**: Safely splits large messages into multiple chunks to respect Slack's 4000-character limit.
- **[streaming_writer.go](streaming_writer.go)**: Implements `io.Writer` for seamless integration between the AI engine's stream and Slack's native streaming UI.

### ⚡ Interactions
- **[slash_commands.go](slash_commands.go)**: Processes slash commands (e.g., `/reset`, `/dc`) with rate limiting and background execution.
- **[interactive.go](interactive.go)**: Handles interactive callbacks from buttons, modals, and other Block Kit elements.

### 🛡️ Security & Reliability
- **[security.go](security.go)**: Provides sanitization, URL validation, and signature verification to ensure secure communication.
- **[validator.go](validator.go)**: Enforces Slack API constraints (character limits, block counts) before sending payloads.
- **[rate_limiter.go](rate_limiter.go)**: Implements per-user token-bucket rate limiting for slash commands.
- **[config.go](config.go)**: Handles workspace configuration, permission policies, and user white/blacklisting.

## Key Features
- **AI-Native UX**: Implements specialized UI for "Thinking", "Tool Use", "Plan Mode", and "Danger Blocks".
- **Real-time Streaming**: Utilizes Slack's new Assistant Threads API for smooth, character-by-character output.
- **Interactive Lifecycle**: Full support for permission requests and human-in-the-loop approvals.
- **Resiliency**: Built-in rate limiting, panic recovery, and payload validation.
