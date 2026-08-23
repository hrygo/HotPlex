# Installation and binary updates

Use the install and update command families only when the operator has
authorized a host change:

    hotplex install --help
    hotplex update --help

Confirm the target version, installation scope, and rollback path before
changing a binary. Verify the installed version and service health after the
change. Windows executable replacement may require the documented installer
path instead of an in-place update.

Never print API keys, service credentials, or complete environment files in a
status report.
