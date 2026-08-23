---
name: hotplex-operator
description: "Operate a HotPlex host: install or restart services, update binaries, change host configuration, inspect audit state, or perform Admin mutations. Use only in an explicitly authorized operator context."
compatibility: Requires local host access, the hotplex CLI, and explicit operator or Admin authority.
---

# HotPlex operator

Use this Skill only for an explicitly authorized host or Admin operation. Read
the narrow reference required for the request, confirm the installed binary
with hotplex <domain> --help, and state the side effect before executing it.

- Service lifecycle: [references/service-lifecycle.md](references/service-lifecycle.md)
- Installation and updates: [references/install-update.md](references/install-update.md)
- Host configuration: [references/configuration.md](references/configuration.md)
- Admin and audit operations: [references/admin-audit.md](references/admin-audit.md)
- Built-in Skill sync and removal: use the authorized operator command and
  verify its status report; this is not a runtime Skill operation.

Do not infer authorization from a diagnostic request. Preserve backups and
report verification results without exposing credentials or private metadata.
