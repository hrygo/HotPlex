---
name: hotplex-cli
description: Use HotPlex CLI for Cron jobs, explicitly requested Slack operations, and read-only status, doctor, security, or config diagnostics. Do not use for Feishu, releases, service installation, binary updates, or Admin mutations.
compatibility: Requires the hotplex CLI and a runtime identity authorized for the requested operation.
---

# HotPlex CLI

Use this Skill as the runtime command router. Read only the reference needed
for the requested operation, then confirm the installed binary with
hotplex <domain> --help before executing a command.

- Cron jobs: [references/cron.md](references/cron.md)
- Explicit Slack operations: [references/slack.md](references/slack.md)
- Read-only diagnostics: [references/diagnostics.md](references/diagnostics.md)
- Public command surface: [references/cli-surface.generated.md](references/cli-surface.generated.md)

Treat command help and the current authorization context as authoritative.
Never expose tokens, cookies, credentials, or unrelated environment values.
