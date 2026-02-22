# @hotplex/sdk

Production-ready TypeScript client for the [HotPlex](https://github.com/hrygo/hotplex) AI Agent Control Plane.

## Installation

```bash
npm install @hotplex/sdk
# or
yarn add @hotplex/sdk
# or
pnpm add @hotplex/sdk
```

## Quick Start

```typescript
import { HotPlexClient, Config } from '@hotplex/sdk';

const client = new HotPlexClient({
  url: 'ws://localhost:8080/ws/v1/agent',
});

const config: Config = {
  work_dir: '/tmp/ai-sandbox',
  session_id: 'my-session',
  task_instructions: 'You are a helpful coding assistant.',
};

for await (const event of client.executeStream(
  'Write a hello world in TypeScript',
  config
)) {
  if (event.type === 'answer') {
    process.stdout.write(String(event.data));
  } else if (event.type === 'thinking') {
    console.log(`\n[Thinking: ${event.data}]`);
  } else if (event.type === 'tool_use') {
    console.log(`\n[Tool: ${event.meta?.tool_name}]`);
  }
}

client.close();
```

## API Reference

### HotPlexClient

```typescript
const client = new HotPlexClient({
  url: 'ws://localhost:8080/ws/v1/agent',
  timeout: 300000,
  reconnect: true,
  reconnectAttempts: 5,
  apiKey: 'your-api-key',
});
```

### execute(prompt, config, onEvent?, timeout?)

Execute a prompt and collect all events.

```typescript
const events = await client.execute(
  'Write a function to calculate fibonacci',
  { work_dir: '/tmp', session_id: 'test' },
  (event) => console.log(event.type),
  60000
);
```

### executeStream(prompt, config, timeout?)

Execute a prompt and yield events as they arrive.

```typescript
for await (const event of client.executeStream(prompt, config)) {
  console.log(event);
}
```

## Error Handling

```typescript
import {
  HotPlexClient,
  DangerBlockedError,
  TimeoutError,
  ExecutionError,
} from '@hotplex/sdk';

try {
  const events = await client.execute('rm -rf /', config);
} catch (e) {
  if (e instanceof DangerBlockedError) {
    console.log('Blocked by WAF:', e.message);
  } else if (e instanceof TimeoutError) {
    console.log('Request timed out');
  } else if (e instanceof ExecutionError) {
    console.log('Execution failed:', e.message);
  }
}
```

## License

MIT
