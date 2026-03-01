# Protocol Specification

## The Duplex Messaging Protocol (DMP)

Communication is not merely the transfer of data; it is the synchronous resonance of two intelligences. HotPlex uses the **Duplex Messaging Protocol (DMP)**—a specialized, high-performance event loop designed for the iterative and often unpredictable nature of AI agent interactions.

---

### The Anatomy of a Pulse

All DMP messages are JSON-serialized "pulses" that follow a strict, immutable schema for absolute reliability.

```json
/* DMP Message Schema */
{
  "id": "msg_01JHGTVR",      // Deterministic message identifier
  "type": "think",           // think | action | output | system
  "timestamp": "2026-03-01T08:21:00Z",
  "payload": {               // Content specific to the message type
    "content": "Analyzing kernel logs..." 
  },
  "metadata": {              // Telemetry and tracing context
    "latency_ms": 12
  }
}
```

---

### The Design of a Conversation

A typical agent interaction is an elegant dance of events, moving through the following states on the Bridge:

1.  **Resonance (Handshake)**: The client establishes continuity and provides cryptographic credentials.
2.  **Harmonics (Input)**: A user prompt enters the system, instantly normalized into a DMP Pulse.
3.  **Thought Cycle (Reflexion)**: The engine emits `think` events as the agent navigates its reasoning space.
4.  **Action Pulse (Reach)**: The agent reaches out to its tools, emitting structured `action` instructions.
5.  **Observation Feedback**: Tool results are fed back into the Thought Cycle, refining the agent's path.
6.  **Resolution (Manifestation)**: The engine emits the final `output` event, completing the cycle.

---

### Structural Resilience

The DMP is engineered for **Sovereign Stability**:

- **Deterministic Sequencing**: Every pulse carries a sequence ID to guarantee correct chronological ordering in high-latency environments.
- **Heartbeat Synchronicity**: A low-level "pulse" maintains the connection's health, triggering immediate state cleanup if the link is severed.
- **State Checkpoints**: The engine performs atomic context saves at every transition, allowing an agent to resume its "thought" from the exact millisecond of a failure.

---

### Technical Implementation

DMP is implemented as a lightning-fast event loop in the HotPlex Go core. It is the invisible architecture that makes AI feel truly alive.

[Explore the core implementation on GitHub](https://github.com/hrygo/hotplex/blob/main/cmd/hotplexd/main.go)
