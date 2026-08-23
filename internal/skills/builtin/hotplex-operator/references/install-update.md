# Installation and binary updates

## Install or initialize

Start from the installed CLI rather than copied package-manager instructions:

    hotplex onboard --help
    hotplex install --help

Use `hotplex onboard` for first-time configuration or reconfiguration. Run
only the mode the operator requested; flags that overwrite configuration,
install a service, or synchronize Skills are separate mutations and require
matching authority. If installing a locally built binary is the request,
inspect `hotplex install --help` and confirm its target before writing.

If no trusted `hotplex` binary is available, stop and ask which official
release artifact or source-build path the operator intends to use. Do not
invent a remote-script pipeline. After installation or initialization, run:

    hotplex doctor

Report failed or warning checks without automatically applying fixes that
were not requested.

## Update

Inspect the local update contract and current version before mutation:

    hotplex update --help
    hotplex update --check

Use `hotplex update` for an authorized binary update. Add `--restart` only
when the request includes a service restart, and add `--sync-skills` only when
the request includes built-in Skill synchronization. When synchronizing,
select the intended profile explicitly and follow the reconciliation checks
in [configuration.md](configuration.md).

Record the pre-update version and expected recovery decision, then verify the
installed version and run `hotplex doctor`. On failure, preserve the updater
and service diagnostics and stop for an operator decision; do not replace the
binary with ad hoc copies, fixed waits, or a Git checkout.
