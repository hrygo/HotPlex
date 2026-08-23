# Read-only diagnostics

Use the read-only HotPlex commands to inspect runtime state:

    hotplex status --help
    hotplex doctor --help
    hotplex security --help
    hotplex config --help
    hotplex service status

Prefer status for a compact health view, doctor for configuration and
dependency checks, security for security posture, config for effective
configuration inspection, and service status for process state. Read-only
logs and Cron history are also appropriate when the installed command exposes
them. Route installation, updates, restarts, configuration writes, and Admin
mutations to hotplex-operator.

When a command offers both read and write modes, select the read-only mode and
report any unavailable check without inventing a result. Never print tokens,
cookies, credentials, arbitrary environment values, or full private payloads.
