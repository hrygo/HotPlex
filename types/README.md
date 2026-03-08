# Global Types

The `types` package contains the fundamental data structures used across the entire HotPlex ecosystem. This shared library ensures type safety and consistency between the **Engine**, **ChatApps**, and **SDK**.

## 🧱 Core Models

- **`Config`**: Global system configuration, including timeouts and security rules.
- **`SessionConfig`**: Per-request session parameters (ID, Provider, Workspace).
- **`StreamMessage`**: The normalized event payload for full-duplex communication.
- **`UsageStats`**: Token consumption and performance metrics.

## 🌊 Message Types

HotPlex uses a unified `MessageType` to categorize interaction intents:

| Type            | Description                                         |
| :-------------- | :-------------------------------------------------- |
| `UserInput`     | Raw text or command from the user.                  |
| `Token`         | A single chunk of AI-generated content (streaming). |
| `FinalResponse` | The complete, finalized message.                    |
| `DangerBlock`   | A security alert triggered by the WAF.              |
| `StatusUpdate`  | Progress indicators (e.g., "AI is searching...").   |

## 📐 Interface Definitions

This package also defines the core interfaces for:
- **`Provider`**: The abstraction layer for AI CLI tools.
- **`Storage`**: Backend persistence contracts for session history.
