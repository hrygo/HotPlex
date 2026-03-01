# TypeScript SDK Mastery

## Full-Stack Synchronicity

The **HotPlex TypeScript SDK** is the bridge between the backend heavy-lifting of AI agents and the fluid, reactive world of modern web interfaces. Built with **type-safety as a first principle**, it enables developers to build "The Manifestation of Interaction" with absolute confidence.

---

### 🌌 Deterministic Agency

In the TypeScript realm, agency is treated as a stream of typed events. We provide a robust client that handles the complexities of full-duplex WebSocket communication, allowing you to focus on the UI/UX of your agentic experience.

- **Reactive Core**: Designed for seamless integration with React, Vue, or Svelte.
- **Type-Sound Interfaces**: Every event, config, and error is strictly typed.
- **Edge Compatible**: Optimized for lightweight execution in serverless or edge environments.

---

### 🌊 The Streaming Flow

Experience the fluidity of real-time agentic interaction.

```typescript
import { HotPlexClient, EventType } from '@hotplex/sdk';

const client = new HotPlexClient({
  url: 'ws://localhost:8080/ws/v1/agent',
  apiKey: 'YOUR_SECRET_PULSE'
});

async function initiateAgent() {
  const session = await client.execute({
    prompt: 'Synthesize the core logic of the Protocol spec.',
    config: {
      sessionId: 'web-session-alpha',
      workDir: '/workspace'
    }
  });

  // Handle the manifestation of events
  session.on(EventType.Thinking, (data) => {
    console.log(`🧠 Agent thought: ${data}`);
  });

  session.on(EventType.Answer, (chunk) => {
    updateUI(chunk); // Fluid, low-latency updates
  });

  session.on(EventType.ToolUse, (meta) => {
    showToolIndicator(meta.toolName);
  });
}
```

---

### 🛡️ Error Surface & Resilience

The SDK is built to handle the uncertainty of the network and the rigor of the HotPlex WAF.

```typescript
import { DangerBlockedError, ConnectionError } from '@hotplex/sdk';

try {
  await initiateAgent();
} catch (err) {
  if (err instanceof DangerBlockedError) {
    notifyUser("Access Restricted: Violation of Security Boundary.");
  } else if (err instanceof ConnectionError) {
    retryConnection();
  }
}
```

---

### 🧩 Universal Deployment

Whether you are building a custom internal dashboard or a public-facing portal, the TypeScript SDK ensures that your agent interactions feel **Premium** and **Native**.

[Master the Slack Integration](/guide/chatapps-slack) or [Explore the Protocol](/reference/protocol)
