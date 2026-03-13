---
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "HotPlex"
  text: "The Strategic Bridge for AI Agent Engineering"
  tagline: Stateful, Secure, and High-Performance Agent Infrastructure.
  image:
    src: /logo.svg
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
    details: Bridge the engine to Slack, Feishu, and custom portals via a unified binding layer.
  - icon: 🛠️
    title: Developer First
    details: Integrate effortlessly using our artisanal Go, Python, and TypeScript SDKs.

---

<style>
:root {
  --hp-primary: #6366f1;
  --hp-primary-soft: rgba(99, 102, 241, 0.1);
  --hp-secondary: #10b981;
  --hp-text-main: #1e293b;
  --hp-text-dim: #64748b;
  --vp-home-hero-name-color: transparent;
  --vp-home-hero-name-background: linear-gradient(135deg, #6366f1 0%, #a855f7 50%, #ec4899 100%);
}

.audience-section {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 24px;
  margin: 48px 0;
}

.audience-card {
  padding: 32px;
  border-radius: 16px;
  background: var(--vp-c-bg-soft);
  border: 1px solid var(--vp-c-divider);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.audience-card:hover {
  transform: translateY(-4px);
  border-color: var(--hp-primary);
  box-shadow: 0 12px 24px -8px rgba(99, 102, 241, 0.2);
}

.audience-card h3 {
  margin: 0 0 12px 0;
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, var(--hp-text-main) 0%, var(--hp-text-dim) 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.audience-card p {
  margin: 0 0 24px 0;
  line-height: 1.6;
  color: var(--hp-text-dim);
  font-size: 15px;
}

.audience-btn {
  display: inline-block;
  padding: 10px 20px;
  border-radius: 8px;
  font-weight: 600;
  font-size: 14px;
  text-align: center;
  background: var(--hp-primary-soft);
  color: var(--hp-primary) !important;
  text-decoration: none !important;
  transition: all 0.2s ease;
}

.audience-btn:hover {
  background: var(--hp-primary);
  color: white !important;
}
</style>

## Choose Your Path

<div class="audience-section">
  <div class="audience-card">
    <h3>End Users</h3>
    <p>Deploy HotPlex agents to Slack, Feishu, or internal portals in minutes.</p>
    <a href="/hotplex/guide/getting-started.html" class="audience-btn">Start Deploying</a>
  </div>
  
  <div class="audience-card">
    <h3>Developers</h3>
    <p>Build custom agent behaviors with our powerful Go, Python, and TS SDKs.</p>
    <a href="/hotplex/reference/api.html" class="audience-btn">View API Reference</a>
  </div>

  <div class="audience-card">
    <h3>Contributors</h3>
    <p>Join the movement! Helping us build the future of stateful agent infra.</p>
    <a href="https://github.com/hrygo/hotplex" class="audience-btn">Join on GitHub</a>
  </div>
</div>

## Artisanal Manuals

<div class="audience-section">
  <div class="audience-card">
    <h3>ChatApps Manual</h3>
    <p>Deep dive into multi-platform connectors, Block Kit mapping, and message pipelines.</p>
    <a href="/hotplex/reference/chatapps.html" class="audience-btn">Explore ChatApps</a>
  </div>
  
  <div class="audience-card">
    <h3>Engine Manual</h3>
    <p>Understand the core control plane, hot-multiplexing pool, and session isolation.</p>
    <a href="/hotplex/reference/engine.html" class="audience-btn">Master the Engine</a>
  </div>

  <div class="audience-card">
    <h3>Provider Manual</h3>
    <p>Learn how to abstract and integrate any AI CLI agent into the HotPlex ecosystem.</p>
    <a href="/hotplex/reference/provider.html" class="audience-btn">Extend Providers</a>
  </div>
</div>

<div style="text-align: center; margin: 60px 0;">
  <h2>Engineering Excellence</h2>
  <p style="max-width: 600px; margin: 0 auto; opacity: 0.8;">
    We focus on the "Boring" parts of AI engineering—state management, reliability, and observability—so you can focus on building the "Magic".
  </p>
</div>

<div align="center">

[![Architecture Overview](/images/topology.svg)](/guide/architecture)

</div>

<div style="width: 100%; overflow: hidden; border-radius: 12px; margin: 40px 0;">
  <img src="/images/hotplex_beaver_banner.webp" alt="HotPlex Mascot Banner" style="width: 100%; height: auto; object-fit: cover;" />
</div>

---

<div align="center">

  [Explore the Architecture](/guide/architecture) · [Quick Start Guide](/guide/getting-started) · [Join the Community](https://github.com/hrygo/hotplex/discussions)

</div>
