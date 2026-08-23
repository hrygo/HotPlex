# Service lifecycle

Use the cross-platform HotPlex service surface rather than platform-native
service-manager commands:

    hotplex service --help
    hotplex service status --help

Before install, uninstall, start, stop, or restart, confirm the requested
service level and disclose the expected interruption. A status or log request
does not authorize a lifecycle mutation.

For a pure restart, use the atomic command:

    hotplex service restart

Never split a restart into stop, wait, and start operations. Inspect status
afterward and run `hotplex doctor` when health is uncertain. Report the
observed level and state rather than assuming Linux, macOS, or Windows service
manager semantics.

If a service is not healthy, preserve the relevant logs and diagnostics. Do
not delete state or retry indefinitely without an operator decision.
