# Issue: Slack Free Plan Compatibility & Fallback Optimization

## 📅 Created: 2026-03-05
## 🏷️ Category: Adapter / Interaction

### 1. ⚠️ Problem Statement
Current Slack AI features (Streaming and Status Bar) rely on Slack's **"Assistant App"** specialized APIs (`chat.startStream`, `assistant.threads.setStatus`). 
Research confirms these are **Paid Features** (Pro/Business+/Enterprise) in production environments. Running on a standard **Slack Free Plan** results in:
- **No Streaming**: Responses appear all at once or fail to send during the streaming phase.
- **No Status Feedback**: Users don't see the "Thinking..." or "Executing tool..." status bar updates, leading to a perceived "hung" application.

### 2. 🎯 Goal
Implement a robust fallback mechanism to ensure a high-quality "Geek Transparency" experience for Slack Free Plan users, while maintaining native premium features for Paid Plan/Developer Sandbox users.

### 3. 🛠️ Proposed Tasks

#### Phase 1: Detection & Infrastructure
- [ ] **Capability Probing**: Attempt a low-cost `assistant.threads.setStatus` call or check `auth.test` plan info at startup to determine capability.
- [ ] **Dynamic Strategy Selection**: Set a `IsAssistantCapable` flag in `slack.Adapter`.

#### Phase 2: Pseudo-Streaming Fallback
- [ ] **Implement `PseudoStreamingWriter`**: If native stream fails, fallback to a writer that uses `chat.postMessage` followed by periodic `chat.update` (debounced to avoid rate limits).
- [ ] **Refactor `NewStreamWriter`**: Factory logic to return the correct implementation.

#### Phase 3: Status Feedback Fallback
- [ ] **UI Fallback Strategy**: 
  - For free-tier, use **Ephemeral Messages** or **In-place Section Updates** for "Thinking" and "Tool Executing" indicators.
  - Implement "Auto-cleanup" of these temporary indicators once the final answer begins.

#### Phase 4: Developer Detail Enhancement (Free-Tier Friendly)
- [ ] **Modal-based Inspector**: Add an "Inspect JSON" button to `ToolResult` blocks.
- [ ] **Implementation**: Since Modals are part of Block Kit and available for free, use `views.open` to show full tool input/output without cluttering history.

### 4. 🧪 Developer Environment & Testing
To develop and test these features without a paid corporate plan, use the **Slack Developer Program Sandbox**:
- [Join Slack Developer Program](https://api.slack.com/developer/program)
- **Enterprise Grid Sandbox**: Provides a free-of-charge environment where all `chat.startStream` and `assistant.*` APIs are enabled for 6 months (renewable).
- **Identity Verification**: May require a credit card for verification, but no charges will be applied.

### 5. 📝 References
- [Slack Free Plan API Limitations](https://slack.com/help/articles/115003205443-Slack-Free-Plan-Details)
- [Slack Assistant App Guide](https://api.slack.com/docs/assistants)
- [Block Kit Modal Documentation](https://api.slack.com/reference/block-kit/blocks)
- [Slack Developer Program FAQ](https://api.slack.com/developer/program)
