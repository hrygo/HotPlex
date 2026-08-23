# Host configuration

Use hotplex config for configuration inspection and the documented setup
workflow for changes. Confirm the effective configuration source before
editing it:

    hotplex config --help
    hotplex doctor --help

Distinguish repository-local development environment files from the runtime
HotPlex home. Preserve unrelated settings, make the smallest authorized
change, and run a read-only doctor or status check afterward.

Do not change global agent configuration to affect one bot. Session
configuration changes take effect at the documented new-session or reset
boundary.
