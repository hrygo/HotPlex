# HotPlex Python SDK

Production-ready Python client for the [HotPlex](https://github.com/hrygo/hotplex) AI Agent Control Plane.

## Installation

```bash
pip install hotplex
```

## Quick Start

```python
from hotplex import HotPlexClient, Config

client = HotPlexClient(url="ws://localhost:8080/ws/v1/agent")

config = Config(
    work_dir="/tmp/ai-sandbox",
    session_id="my-session",
    task_instructions="You are a helpful coding assistant."
)

for event in client.execute_stream(
    prompt="Write a Python function to calculate fibonacci",
    config=config,
):
    if event.type == "answer":
        print(event.data, end="")
    elif event.type == "thinking":
        print(f"\n[Thinking: {event.data}]")
    elif event.type == "tool_use":
        print(f"\n[Tool: {event.meta.tool_name}]")

client.close()
```

## Context Manager

```python
from hotplex import HotPlexClient, Config

with HotPlexClient() as client:
    events = client.execute(
        prompt="List files in current directory",
        config=Config(work_dir="/tmp", session_id="test")
    )
    
    for event in events:
        print(f"{event.type}: {event.data}")
```

## Error Handling

```python
from hotplex import (
    HotPlexClient,
    Config,
    DangerBlockedError,
    TimeoutError,
    ExecutionError,
)

client = HotPlexClient()

try:
    events = client.execute(
        prompt="rm -rf /",
        config=Config(work_dir="/tmp", session_id="test")
    )
except DangerBlockedError as e:
    print(f"Blocked by WAF: {e}")
except TimeoutError as e:
    print(f"Request timed out: {e}")
except ExecutionError as e:
    print(f"Execution failed: {e}")
```

## Configuration Options

```python
from hotplex import HotPlexClient, ClientConfig

config = ClientConfig(
    url="ws://localhost:8080/ws/v1/agent",
    timeout=300.0,
    reconnect=True,
    reconnect_attempts=5,
    reconnect_delay=1.0,
    log_level="DEBUG",
    api_key="your-api-key",
)

client = HotPlexClient(config=config)
```

## Event Types

| Event | Description |
|-------|-------------|
| `thinking` | Agent is thinking |
| `answer` | Streaming text response |
| `tool_use` | Tool invocation started |
| `tool_result` | Tool execution result |
| `session_stats` | Final session statistics |
| `error` | Error occurred |
| `danger_block` | Blocked by WAF |

## License

MIT
