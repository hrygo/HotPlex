# Admin and audit operations

Separate read-only evidence gathering from state changes. Inspect the current
surface before acting:

    hotplex admin --help
    hotplex audit --help

Audit verification and authorized Admin read endpoints may support diagnosis.
Account creation, audit-chain repair, and Admin API writes are mutations and
require explicit authority for the target host and operation.

Before a write, state the target, mutation, expected side effect, and recovery
boundary. Afterward, verify through the narrowest read-only audit or status
view and report the mutation and outcome. Do not load, print, or embed bearer
tokens, cookies, credentials, or complete environment files merely to
demonstrate an authenticated request.
