# Service lifecycle

Use the service command family only with explicit operator authorization:

    hotplex service --help
    hotplex service status
    hotplex service restart

Prefer the atomic restart command when a restart is requested. Inspect status
afterward and report whether the process became healthy. Do not split a
restart into an unbounded stop, sleep, and start sequence.

If a service is not healthy, preserve the relevant logs and diagnostics. Do
not delete state or retry indefinitely without an operator decision.
