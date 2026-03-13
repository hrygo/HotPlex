# Architecture Overview

## The Anatomy of a Stateful Agent

HotPlex isn't just a runtime; it's a **designed nervous system** for AI agents. Our architecture is built upon the "Strategic Bridge" principle—connecting raw LLM capabilities with the stability and security required for enterprise production.

---

### The Core Triad

The HotPlex engine is built around three fundamental pillars:

#### 1. 🧠 Stateful Persistence
Agents shouldn't be amnesiac. HotPlex provides a high-reliability state layer that persists:
- **Conversation History**: Every turn is tracked and managed.
- **Agent Context**: Long-term "memories" and learned preferences.
- **Session Continuity**: Seamlessly resume sessions across different platforms.

#### 2. 🛡️ Security Sandbox
Running untrusted agent logic is a risk. HotPlex isolates every execution in a multi-layered sandbox:
- **Resource Limits**: Control CPU and memory usage.
- **File Access Isolation**: Prevent unauthorized local filesystem access.
- **API Guarding**: Fine-grained control over which external APIs an agent can call.

#### 3. ⚡ Duplex Stream Engine
Communication between agents, tools, and users must be instantaneous. Our proprietary streaming protocol delivers:
- **Low Latency**: Sub-millisecond response for event-driven updates.
- **Bi-directional Flow**: Real-time feedback from tools back to the agent while the user is still watching.

---

### High-Level Topology

![Architecture Overview](/images/topology.svg)

- **The Engine**: Orchestrates the agent lifecycle and executes core logic.
- **Plugins/Hooks**: The extensibility layer where developers inject custom behaviors.
- **ChatApps**: Adapters that bridge the engine to user-facing platforms like Slack or Feishu.

---

### Technical Rigor: Built for Scale

HotPlex is written in **Go**, choosing performance over abstraction. Every component—from the event loop to the persistence layer—is optimized for high-throughput, ensuring that your agent infrastructure can scale alongside your user base.

[Dive deeper into the SDKs](/sdks/go-sdk) or [Explore the Hooks System](/guide/hooks).
