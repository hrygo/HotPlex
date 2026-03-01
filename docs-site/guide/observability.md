# Observability

## Transparency in the Age of Agents

Debugging AI agents can be a nightmare of black boxes and stochastic behavior. HotPlex solves this by building **Observability** directly into the core engine. Every thought, action, and failure is recorded and visible.

---

### The Three Pillars of Agent Visibility

#### 1. 🔍 Real-time Trace Logs
Every session in HotPlex generates a structured trace. You can follow an agent's logic in real-time via the CLI or the Admin Dashboard:
- **Trace Level**: See exact token counts and model latency.
- **Action Level**: Track every tool call and its raw result.

#### 2. 📊 Performance Metrics
HotPlex exposes standard **Prometheus metrics**, allowing you to monitor the health of your agent infrastructure:
- **P99 Latency**: Measure how fast your agents are responding.
- **Error Rates**: Identify which hooks or tools are failing most frequently.
- **Token Usage**: Track model costs in real-time across your entire fleet.

#### 3. 🛡️ Audit Trails
For enterprise compliance, every interaction is recorded in an immutable audit log:
- **Who**: Which user or system triggered the session.
- **What**: The exact input, thought process, and output.
- **Where**: The ChatApp or API endpoint used.

---

### Integration with External Tools

HotPlex is designed to play well with the modern observability stack:

- **OpenTelemetry**: Native support for exporting traces to Jaeger, Honeycomb, or AWS X-Ray.
- **Grafana**: Pre-built dashboards for monitoring agent health and performance.
- **ELK Stack**: Stream structured logs directly to Elasticsearch for deep analysis.

```bash
# Export traces to an OTLP collector
hotplexd run --otel-endpoint "otel.internal.yourcorp.com:4317"
```
