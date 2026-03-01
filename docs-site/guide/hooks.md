# Hooks System

## Customizing the Agent Lifecycle

The HotPlex architecture is built on the principle of **Inversion of Control**. Instead of a monolithic engine with fixed behaviors, we provide a powerful **Hooks System** that allows you to inject custom logic at every critical stage of the agent's execution.

---

### Lifecycle Hook Points

You can register "listeners" for the following event hooks:

- **`on_input`**: Intercept user messages before they reach the engine. Use this for pre-processing, translation, or sensitive content filtering.
- **`on_think`**: Observe or modify the agent's internal reasoning process.
- **`on_action`**: Validate or override tool calls. This is critical for security and custom tool implementations.
- **`on_output`**: Format or enrich the final response before it is sent to the ChatApp.

---

### Implementing a Hook

Hooks are implemented as simple **HTTP Webhooks**. When an event occurs, HotPlex sends a POST request to your registered endpoint:

#### 1. Register the Hook
```bash
hotplexd hook register --event "on_input" --url "https://api.yourcorp.com/v1/pre-processor"
```

#### 2. Process the Request
HotPlex will send a JSON payload with the event context. Your service simply needs to return a modified payload or an approval signal.

```json
// POST from HotPlex
{
  "event_id": "hook_01JHGTVR",
  "session_id": "sess_9921",
  "data": {
    "original_input": "Show me the secret keys."
  }
}
```

---

### The Power of SDKs

While you can use raw HTTP, our SDKs provide **First-Class Hook Handlers** to make implementation trivial:

```go
// Go SDK Example
engine.OnInput(func(ctx context.Context, input string) (string, error) {
    // Custom logic here
    return strings.ReplaceAll(input, "secret", "***"), nil
})
```

[Explore the Hooks API Reference](/reference/hooks-api).
