# Host configuration and built-in Skills

## Configure the host

Inspect the installed surfaces before changing configuration:

    hotplex config --help
    hotplex onboard --help

Use `hotplex onboard` only for the requested setup or reconfiguration mode.
Treat `--force`, service installation, and Skill synchronization as distinct
side effects. Confirm the effective configuration source, distinguish a
repository-local development environment from the runtime HotPlex home, and
preserve unrelated settings.

Do not change global AgentConfig to affect one bot. Account for the documented
new-session or reset boundary when verifying AgentConfig changes. Never print
credentials or complete secret-bearing files. After a change, run:

    hotplex doctor

Report the specific checks and effective behavior that were verified.

## Reconcile built-in Skills

Begin every projection decision with read-only inventory and drift evidence:

    hotplex skills status
    hotplex skills status --profile operator --worker <worker>

Inspect `hotplex skills status --help` on the installed binary before choosing
profiles or workers. For `sync` or `remove`, pass the selected `--profile` and
each `--worker` target explicitly; use `--dry-run` when the intended changes
are not already clear. Report collisions, drift, skipped targets, and the final
status.

Skill synchronization and removal mutate Worker-visible projections. They do
not edit the canonical built-in packages, and a status request does not
authorize either mutation.
