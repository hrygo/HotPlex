# Event Protocol

The `event` package defines the high-performance communication protocol for HotPlex. It provides the callback mechanisms and metadata structures required for real-time AI interaction.

## 📡 Event Models

- **`Event`**: The base structure for all system events.
- **`EventMetadata`**: Contextual information including timestamps, session IDs, and trace IDs.
- **`Callback`**: A unified function signature used throughout the codebase for event handling.

## 🔄 Interaction Pattern

HotPlex events follow a **Streamed Observer** pattern:

1. **Dispatch**: The Engine generates events (tokens, status updates, security blocks).
2. **Metadata Injection**: The event is wrapped with session-specific metadata.
3. **Execution**: Registered callbacks (CLI, WebSocket, or ChatApps) process the event in real-time.

## 🛠 Practical Usage

```go
// Register a simple observer
engine.Execute(ctx, cfg, prompt, func(ev event.Event) {
    switch ev.Type {
    case types.MessageTypeToken:
        processToken(ev)
    case types.MessageTypeDangerBlock:
        triggerSecurityAlert(ev)
    }
})
```
