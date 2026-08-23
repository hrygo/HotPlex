---
name: hotplex-operator
description: "Operate a HotPlex host: install or restart services, update binaries, change host configuration, inspect audit state, or perform Admin mutations. Use only in an explicitly authorized operator context."
compatibility: Requires local host access, the hotplex CLI, and explicit operator or Admin authority.
---

# HotPlex operator

Use this Skill only for an explicitly authorized host or Admin operation.
Before a mutation, inspect the installed command's `--help`, identify the
target and impact, and confirm the request authorizes that exact side effect.

- Service lifecycle: [references/service-lifecycle.md](references/service-lifecycle.md)
- Installation and updates: [references/install-update.md](references/install-update.md)
- Host configuration and built-in Skill reconciliation: [references/configuration.md](references/configuration.md)
- Admin and audit operations: [references/admin-audit.md](references/admin-audit.md)

Do not infer mutation authority from diagnosis, installation intent from a
configuration request, or restart authority from an update check. Report the
result and remaining risk without exposing credentials or private metadata.
