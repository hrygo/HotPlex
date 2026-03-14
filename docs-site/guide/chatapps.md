# ChatApps Ecosystem

## The Manifestation of Interaction

The true power of an AI agent is not found in the code alone, but in its ability to manifest where the user lives. **ChatApps** are the specialized receptors that bridge the HotPlex engine to the world's most powerful communication platforms.

---

### The Ecosystem Map

We prioritize platforms where conversation and work converge. Our adapters are designed to be more than just "chatbots"—they are native extensions of the host environment.

| Platform       | Soul                    | Status       | Capabilities                        |
| :------------- | :---------------------- | :----------- | :---------------------------------- |
| **Slack**      | **Primary Receptor**    | ✅ Production | Full Block Kit, Real-time Reactions |
| **Web Portal** | Absolute Sovereignty    | ✅ Production | Glassmorphism UI, Custom Branding   |

---

### The "Sovereign Interaction" Model

Unlike traditional webhooks that suffer from statelessness, HotPlex ChatApps maintain a **Continuous Duplex Stream**. This ensures:

- **Cognitive Transparency**: Users see the agent's internal reasoning (`thinking`) and tool selections in real-time.
- **Visual Action Zones**: Complex interactions—like code diffs or permission requests—are rendered as interactive visual blocks.
- **Stateful Mobility**: Conversations are persistent. A session born in Slack can be resumed in the Web Portal with zero context loss.

---

### The Anatomy of a Binding

Integrating a platform is no longer a matter of complex boilerplate. It is a declaration of intent via environment configuration.

1.  **Identity**: Register your app on the target platform (e.g., Slack App Portal).
2.  **Continuity**: Provide the platform tokens to your HotPlex environment.
3.  **Activation**: The `hotplexd` daemon automatically initializes the receptors based on your `.env` configuration.

```bash
# Activation is driven by environmental variables
# See the Slack Mastery Guide for details.
HOTPLEX_SLACK_ENABLED=true
HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
```

---

### ChatApps Configuration

#### Core Environment Variables

| Variable | Description |
| :------- | :---------- |
| `HOTPLEX_CHATAPPS_ENABLED` | Enable/disable all ChatApps |
| `HOTPLEX_CHATAPPS_CONFIG_DIR` | Directory for platform configs |
| `HOTPLEX_FEISHU_ENABLED` | Enable Feishu (飞书) adapter |
| `HOTPLEX_FEISHU_APP_ID` | Feishu App ID |
| `HOTPLEX_FEISHU_APP_SECRET` | Feishu App Secret |
| `HOTPLEX_FEISHU_VERIFICATION_TOKEN` | Feishu verification token |
| `HOTPLEX_DINGTALK_ENABLED` | Enable DingTalk (钉钉) adapter |

#### Configuration Directory

ChatApps can be configured via YAML files in a config directory:

```bash
export HOTPLEX_CHATAPPS_CONFIG_DIR=/etc/hotplex/chatapps
```

Place platform-specific configs in this directory:
- `slack.yaml` - Slack configuration
- `feishu.yaml` - Feishu configuration
- `dingtalk.yaml` - DingTalk configuration

---

### The Vision: Ubiquitous Intelligence

Our goal is a **Unified Agentic Surface**. You build the logic once; HotPlex ensures that every interaction is perfectly tailored to the unique aesthetics and capabilities of the platform.

[Master the Slack Integration](/guide/chatapps-slack) or [Explore the Protocol](/reference/protocol)
