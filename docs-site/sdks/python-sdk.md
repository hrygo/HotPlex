# Python SDK Mastery

## The AI-First Execution Surface

The **HotPlex Python SDK** is crafted for the modern AI engineer. It transforms elite CLI agents into long-lived, interactive services (**Cli-as-a-Service**), allowing your Python applications to orchestrate agentic workflows with the elegance of native context management.

---

### 🔋 Powering Contextual Intelligence

In the Python ecosystem, we focus on minimizing friction. The SDK manages the underlying WebSocket streams, providing a clean, vegetable interface for the agent's cognitive output.

- **Unified Stream**: A single generator for thinking, tool usage, and final artifacts.
- **Context Awareness**: Native support for Python's `with` statement ensuring resource sovereignty.
- **WAF-Integrated**: Direct handling of `DangerBlockedError` for secure execution.

---

### 🏁 The Quick-Start Journey

Integrate HotPlex into your agentic pipeline in seconds.

```python
from hotplex import HotPlexClient, Config, DangerBlockedError

# Establish a sovereign connection
with HotPlexClient(url="ws://localhost:8080/ws/v1/agent") as client:
    config = Config(
        work_dir="/app/research",
        session_id="research-session-42",
        task_instructions="Analyze the market trends in metadata."
    )

    try:
        # Stream the manifestation of intelligence
        for event in client.execute_stream("Draft a summary of Q1 trends.", config):
            if event.type == "thinking":
                print(f"🧠 {event.data}")
            elif event.type == "answer":
                print(event.data, end="")
            elif event.type == "tool_use":
                print(f"\n[Invoking: {event.meta.tool_name}]")
                
    except DangerBlockedError as e:
        print(f"⚠️ Security Boundary Reached: {e}")
```

---

### 🧩 Advanced Configuration

For production environments, fine-tune the receptor's connection logic.

```python
from hotplex import HotPlexClient, ClientConfig

config = ClientConfig(
    url="ws://hotplex.internal/v1/agent",
    api_key="HPLX_PROD_TOKEN",
    reconnect=True,
    reconnect_attempts=10,
    timeout=600.0  # Support for long-running cognitive tasks
)

client = HotPlexClient(config=config)
```

---

### 🌐 OpenCode (SSE) Compatibility

For environments where WebSockets are restricted, the Python SDK provides a native `OpenCodeClient` using high-performance Server-Sent Events (SSE).

```python
from hotplex import OpenCodeClient

client = OpenCodeClient(url="http://hotplex.internal")
# Usage mirrors the WebSocket client for a seamless developer journey.
```

[Integrate with Slack](/guide/chatapps-slack) or [See the Ecosystem Gallery](/guide/chatapps)
