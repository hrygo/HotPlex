# First-time initialization

Use this reference for a new host, a missing configuration, or an explicitly
requested reconfiguration. It describes the complete handoff from an
installed binary to a verified Gateway; every write still requires explicit
operator authorization.

## Desired outcome

Initialization is complete only when the requested scope has:

- a trusted `hotplex` binary available through the intended PATH;
- `config.yaml` and `.env` generated or deliberately preserved;
- the selected Worker and requested messaging platforms configured;
- `hotplex doctor` run after configuration, with failures resolved or warnings
  recorded;
- the requested service level installed, started, and checked, or a deliberate
  foreground/development start selected;
- the installed version, Gateway status, Worker evidence, and remaining risks
  reported without exposing secrets.

## Workflow

### 1. Establish the target

Confirm whether this is a fresh install, a reconfiguration, a development
run, or a service deployment. Inspect the installed surface before mutating:

    hotplex version --help
    hotplex install --help
    hotplex onboard --help
    hotplex doctor --help
    hotplex service --help

Record the intended config path, service level (`user` or `system`), Worker,
platforms, and whether the request includes service installation, Skill
synchronization, or only local configuration. Do not infer any of these from
the current machine.

### 2. Make the binary available

If the command is already trusted and resolves to the intended binary, record
`hotplex version` and continue. If it is absent or points elsewhere, use the
selected official artifact or the repository's documented source-build path;
then inspect and run `hotplex install` only when the operator authorized that
installation target. Never invent a remote-script installer or replace an
existing binary with an unreviewed file.

### 3. Run `onboard`

Use interactive setup for a first-time host unless automation is explicitly
requested:

    hotplex onboard

For automation, pass `--non-interactive` and specify the requested platform
flags and policies. Treat these as separate, reviewable effects:

- `--force` can overwrite existing configuration;
- `--enable-slack` / `--enable-feishu` enable platform blocks and require the
  required credentials in the effective `.env` source;
- `--install-service --service-level user|system` installs a service;
- `--sync-skills` writes Worker-visible built-in Skill projections.

The wizard also selects the Worker, creates or preserves AgentConfig files,
checks STT/TTS prerequisites, and writes configuration with secret-bearing
files protected. Report missing credentials or optional dependency warnings;
do not print their values or automatically enable an unrequested platform.

### 4. Interpret `doctor`

Run the full read-only report after `onboard`:

    hotplex doctor --json

Group each `fail` and `warn` by the category named in the current report
(`environment`, `config`, `dependencies`, `security`, `runtime`, `messaging`,
`stt`, `tts`, `agent_config`, `skills`, or `worker`). Resolve only the items
covered by the user's authorization, then rerun the report. `--fix` is a
mutation and requires separate explicit approval; a clean configuration check
does not prove that a service or platform connection is healthy.

### 5. Install and start the service when requested

Installing a service is not the same verification as observing a running
Gateway. If `onboard` did not use `--install-service`, install it first; if it
did, skip the duplicate install and continue with start/status. Use the same
explicit level for every command:

    hotplex service install --level user
    hotplex service start --level user
    hotplex service status --level user --json
    hotplex service logs --level user --lines 100

Use `--level system` only with the required elevated authority. After
`onboard --install-service`, still inspect status and start the service when
the platform has not started it automatically. If the user chose a foreground
or development run instead, use the current `hotplex gateway --help` surface
and verify it separately; do not install a service as an implicit fallback.

### 6. Verify and hand off

At minimum, verify the installed version, `doctor` result, service status (if
applicable), recent startup logs, selected Worker evidence, and requested
platform state. A service manager reporting `active` is not enough if logs
show configuration, Worker, or messaging failures. Report the exact checks,
observed status, unresolved warnings, and the next authorized action.

## Failure boundaries

- Missing binary, unsupported OS, missing Worker, invalid config, or missing
  required credentials blocks completion; do not claim the host is ready.
- A failed service start requires status and logs, not repeated restart loops.
- STT/TTS and platform setup are optional only when the user did not request
  those capabilities; otherwise their warnings remain part of the outcome.
- Preserve existing configuration and backups. Do not delete state, change
  global AgentConfig to affect one bot, or roll back a binary without a new
  explicit authorization.
