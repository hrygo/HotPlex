# API Reference

## Building with the HotPlex Runtime

The HotPlex API is designed for high-performance agentic interactions. It is the interface through which the "Magic" is orchestrated. We provide two distinct planes of interaction: a **RESTful Control Plane** for structural management and a **Streaming Data Plane** for real-time cognitive execution.

---

### Authentication

All API requests must include a Bearer token in the `Authorization` header. This is the cryptographic key to the Bridge.

```http
Authorization: Bearer [HOTPLEX_API_KEY]
```

---

### The REST Control Plane

Manage the structural state of your agents programmatically. The Control Plane is designed for stability and observability.

| Endpoint       | Method | Vision                                            |
| :------------- | :----- | :------------------------------------------------ |
| `/session`     | `POST` | Initialize a new stateful agent context.          |
| `/session/:id` | `GET`  | Inspect the deep context and memory of a session. |
| `/v1/bindings` | `GET`  | Enumerate all active multi-platform receptors.    |
| `/v1/hooks`    | `POST` | Inject custom reflexes into the agent lifecycle.  |

#### Example: Initialize a Session
```json
/* POST /session */
{
  "name": "coding-assistant",     // Unique identifier for the context
  "template": "standard-oracle",  // The base behavioral model
  "metadata": {
    "project": "hotplex-docs"      // Custom telemetry tags
  }
}
```

---

### The Streaming Data Plane

For real-time agent execution, HotPlex utilizes a **Duplex WebSocket** connection. This is the high-speed nervous system where the agent's thought cycles are streamed directly to the user.

#### URI Pattern
`ws://[HOTPLEX_HOST]/ws/v1/agent`

#### Cognitive Event Types
- `think`: The agent is navigating its internal reasoning space.
- `action`: The agent is reaching out to the world via a tool.
- `output`: Final or intermediate intellectual artifacts.
- `error`: Diagnostic feedback from the engine core.

---

### Beyond the Raw API

While the API is the foundation, our official SDKs provide an artisanal layer of abstraction for a more fluid developer experience.

<div class="audience-section">
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Go SDK</h3>
    <a href="/sdks/go-sdk" class="audience-btn">Go Deep</a>
  </div>
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Python SDK</h3>
    <a href="/sdks/python-sdk" class="audience-btn">Go Rapid</a>
  </div>
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>TS SDK</h3>
    <a href="/sdks/typescript-sdk" class="audience-btn">Go Flux</a>
  </div>
</div>

> "Code should be as beautiful as the logic it represents." — The HotPlex Team
