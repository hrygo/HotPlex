# The Anatomy of a Stateful Agent

## A Designed Nervous System for AI

HotPlex isn't just a runtime; it's a **nervous system**. While raw LLMs provide the "brain," HotPlex provides the reflexes, memory, and skin that allow an agent to survive and thrive in production.

---

### A Day in the Life of a Packet

To understand HotPlex, follow a single event—a user message—as it journeys across the **Strategic Bridge**.

1.  **Incoming Resonance**: An event (Slack Mention, API Call) hits the **ChatApp Adapter**. It's instantly normalized into the unified **HotPlex Protocol**.
2.  **The Persistence Gate**: Before the agent even "thinks," the **State Manager** retrieves the last 50 turns of context. Continuity is established in milliseconds.
3.  **The Secure Crucible**: The agent logic is loaded into the **Sandbox Container**. Here, it can run code and call tools, but its reach is strictly bounded by the **Security Guard**.
4.  **The Duplex Loop**: As the agent reasons, it streams live updates back to the user via the **Duplex Stream Engine**. No waiting for whole blocks—the conversation is alive.

---

### The Pillars of Structural Integrity

<div class="audience-section">
  <div class="audience-card">
    <h3>Continuity Layer</h3>
    <p>We solve the "Amnesia Problem." By treating state as a first-class citizen, HotPlex ensures that agents remain context-aware across platforms and restarts.</p>
  </div>
  
  <div class="audience-card">
    <h3>Sovereign Isolation</h3>
    <p>Agents need tools, but tools are dangerous. Our multi-layered sandbox provides the freedom of local execution with the safety of a zero-trust environment.</p>
  </div>

  <div class="audience-card">
    <h3>Engine Reactivity</h3>
    <p>Built in Go, our binary-powered core is designed for sub-millisecond event loops. In HotPlex, performance is the foundation of intelligence.</p>
  </div>
</div>

---

### High-Level Topology

![Architecture Overview](/images/topology.svg)

- **The Engine**: The beating heart that orchestrates the agent lifecycle.
- **Hooks & Plugins**: The nervous system's extensions where you inject custom "reflexes."
- **ChatApp Adapters**: The sensory organs that connect to the outside world.

---

### Technical Rigor: Built for Scale

HotPlex is optimized for high-throughput, ensuring that your agent infrastructure can scale alongside your user base without sacrificing the "magic" of low-latency interaction.

[Continue to the Hooks API](/reference/hooks-api) or [Master the Protocol](/reference/protocol).
