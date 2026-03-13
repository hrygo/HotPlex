# ChatApps Reference

The `chatapps` package provides the bridge between HotPlex's core engine and various chat platforms. It normalizes platform-specific events and messages into a unified "Chat Language".

## 🔄 End-to-End Bidirectional Flow

<div class="architecture-diagram" style="margin: 2rem 0; border-radius: 16px; overflow: hidden; background: #0F172A; box-shadow: 0 10px 30px -10px rgba(0,0,0,0.5);">
  <img src="/images/chatapps_flow.svg" alt="HotPlex Architecture Flow" style="display: block; width: 100%; height: auto;" />
</div>

## 🏛 Architecture Overview

HotPlex uses an **Adapter-based Pipeline** architecture.

### Data Normalization

The `chatapps` layer normalizes raw provider events into standard UI components.

| Provider Event       | `base.MessageType`             | UI Presentation     |
| :------------------- | :----------------------------- | :------------------ |
| `thinking`           | `MessageTypeThinking`          | Thinking bubbles    |
| `tool_use`           | `MessageTypeToolUse`           | Tool info block     |
| `tool_result`        | `MessageTypeToolResult`        | Collapsible output  |
| `answer`             | `MessageTypeAnswer`            | Markdown text       |
| `permission_request` | `MessageTypePermissionRequest` | Interactive buttons |

### Key Architectural Concepts

-   **`ChatAdapter`**: The platform-specific connector logic.
-   **`AdapterManager`**: Singleton for managing active connections.
*   **`ProcessorChain`**: Middleware-style pipeline for message styling and filtering.

---

## 🛠 Developer Guide

### 1. Implementing a New Platform Adapter

To add a new platform adapter, implement the `base.ChatAdapter` interface:

---

## 🏗️ Connect More Platforms

<div class="audience-section">
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Slack Guide</h3>
    <p>Step-by-step Slack bot creation and Block Kit setup.</p>
    <a href="/hotplex/guide/chatapps-slack.html" class="audience-btn">View Slack</a>
  </div>
  <div class="audience-card" style="padding: 24px; min-width: 200px;">
    <h3>Engine Manual</h3>
    <p>Understand how messages are processed by the core.</p>
    <a href="/hotplex/reference/engine.html" class="audience-btn">View Engine</a>
  </div>
</div>

> "Interfaces are the grammar of software architecture." — The HotPlex Core Team
