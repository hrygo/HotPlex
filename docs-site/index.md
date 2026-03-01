---
<!-- CI Trigger: Checking asset generation permissions -->
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "HotPlex"
  text: "The Strategic Bridge for AI Agent Engineering"
  tagline: Stateful, Secure, and High-Performance Agent Infrastructure.
  image:
    src: /images/mascot_primary.svg
    alt: HotPlex Primary Mascot
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/hrygo/hotplex

features:
  - icon: 🧠
    title: Stateful Orchestration
    details: Manage complex agent states with built-in persistence and context awareness.
  - icon: 🛡️
    title: Enterprise Security
    details: Built with security-first principles, supporting fine-grained access control and auditing.
  - icon: 💬
    title: Native Slack Experience
    details: Production-ready Slack integration with Block Kit mapping, real-time typing indicators, and session summaries.
  - icon: ⚡
    title: High Performance
    details: Optimized for low-latency agent interactions and high-throughput event processing.
  - icon: 🔌
    title: Extensible Ecosystem
    details: Bridge the engine to DingTalk, Discord, and custom portals via a unified binding layer.
  - icon: 🛠️
    title: Developer First
    details: Integrate effortlessly using our artisanal Go, Python, and TypeScript SDKs.

---

<style>
:root {
  --vp-home-hero-name-color: transparent;
  --vp-home-hero-name-background: -webkit-linear-gradient(120deg, #bd34fe 30%, #41d1ff);
}
</style>

## Choose Your Path

<div class="audience-section">
  <div class="audience-card">
    <h3>End Users</h3>
    <p>Deploy HotPlex agents to Slack, DingTalk, or internal portals in minutes.</p>
    <a href="/guide/getting-started" class="audience-btn">Start Deploying</a>
  </div>
  
  <div class="audience-card">
    <h3>Developers</h3>
    <p>Build custom agent behaviors with our powerful Go, Python, and TS SDKs.</p>
    <a href="/reference/api" class="audience-btn">View API Reference</a>
  </div>

  <div class="audience-card">
    <h3>Contributors</h3>
    <p>Join the movement! Helping us build the future of stateful agent infra.</p>
    <a href="https://github.com/hrygo/hotplex" class="audience-btn">Join on GitHub</a>
  </div>
</div>

---

<div style="text-align: center; margin: 60px 0;">
  <h2>Engineering Excellence</h2>
  <p style="max-width: 600px; margin: 0 auto; opacity: 0.8;">
    We focus on the "Boring" parts of AI engineering—state management, reliability, and observability—so you can focus on building the "Magic".
  </p>
</div>

[![Architecture Overview](/images/topology.svg)](/guide/architecture)

---

[Explore the Architecture](/guide/architecture) · [Quick Start Guide](/guide/getting-started) · [Join the Community](https://github.com/hrygo/hotplex/discussions)
