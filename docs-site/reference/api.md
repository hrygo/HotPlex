# API Reference

## Building with the HotPlex Runtime

The HotPlex API is designed for high-performance agentic interactions. It is the interface through which the "Magic" is orchestrated. We provide two distinct planes of interaction: a **RESTful Control Plane** for structural management and a **Streaming Data Plane** for real-time cognitive execution.

---

### Authentication

All API requests must include a Bearer token in the `Authorization` header or `X-API-Key` header. This is the cryptographic key to the Bridge.

```http
Authorization: Bearer [HOTPLEX_API_KEY]
# or
X-API-Key: [HOTPLEX_API_KEY]
```

Enable authentication via environment variable:
```bash
export HOTPLEX_API_KEY=your-secret-key
```

---

### Health & Observability

| Endpoint | Method | Description |
| :------- | :----- | :---------- |
| `/health` | `GET` | Basic health check |
| `/health/ready` | `GET` | Readiness probe (checks if ready to serve requests) |
| `/health/live` | `GET` | Liveness probe (checks if process is running) |
| `/metrics` | `GET` | Prometheus metrics endpoint |

```bash
# Check health
curl http://localhost:8080/health

# Check readiness
curl http://localhost:8080/health/ready

# Get metrics
curl http://localhost:8080/metrics
```

---

### The REST Control Plane

Manage the structural state of your agents programmatically. The Control Plane is designed for stability and observability.

> [!NOTE]
> These endpoints are compatible with the **OpenCode Protocol**.

| Endpoint | Method | Description |
| :------- | :----- | :---------- |
| `/session` | `POST` | Initialize a new stateful agent context |
| `/session/{id}/message` | `POST` | Send a prompt to an existing session |
| `/session/{id}/prompt_async` | `POST` | Send a prompt asynchronously |
| `/config` | `GET` | Get server configuration |
| `/global/event` | `GET` | Server-Sent Events stream for session events |

#### Example: Create a Session

```bash
# POST /session
curl -X POST http://localhost:8080/session \
  -H "Content-Type: application/json"
```

Response:
```json
{
  "info": {
    "id": "uuid-string",
    "projectID": "default",
    "directory": "/tmp/hotplex",
    "title": "New Session",
    "time": {
      "created": 1739331200000
    }
  }
}
```

#### Example: Send a Message

```bash
# POST /session/{id}/message
curl -X POST http://localhost:8080/session/{id}/message \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Hello, help me write a function",
    "system_prompt": "You are a helpful coding assistant"
  }'
```

---

### The Streaming Data Plane

For real-time agent execution, HotPlex utilizes a **Duplex WebSocket** connection. This is the high-speed nervous system where the agent's thought cycles are streamed directly to the user.

#### URI Pattern
`ws://[HOST]:[PORT]/ws/v1/agent`

#### WebSocket Client Example

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

ws.onopen = () => {
  console.log('Connected to HotPlex');
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data);
};
```

#### Cognitive Event Types

| Event | Description |
| :---- | :---------- |
| `message.part.updated` | Agent output (text, reasoning, tool use) |
| `server.connected` | WebSocket connection established |

##### Message Part Structure

```json
{
  "type": "message.part.updated",
  "properties": {
    "part": {
      "id": "message-uuid",
      "sessionID": "session-uuid",
      "messageID": "msg-uuid",
      "type": "text|reasoning|tool",
      "text": "Agent output content",
      "tool": { /* tool details */ },
      "state": {
        "status": "running|completed",
        "input": { /* tool input */ },
        "output": "tool result"
      }
    }
  }
}
```

---

### SSE Events

Subscribe to server events via Server-Sent Events (SSE):

```bash
# GET /global/event (requires auth if API key enabled)
curl -N http://localhost:8080/global/event \
  -H "Authorization: Bearer your-api-key"
```

---

### Beyond the Raw API

While the API is the foundation, our official SDKs provide an artisanal layer of abstraction for a more fluid developer experience.

<div class="audience-section">
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Go SDK</h3>
    <a href="/hotplex/sdks/go-sdk.html" class="audience-btn">Go Deep</a>
  </div>
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Python SDK</h3>
    <a href="/hotplex/sdks/python-sdk.html" class="audience-btn">Go Rapid</a>
  </div>
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>TS SDK</h3>
    <a href="/hotplex/sdks/typescript-sdk.html" class="audience-btn">Go Flux</a>
  </div>
</div>

> "Code should be as beautiful as the logic it represents." — The HotPlex Team
