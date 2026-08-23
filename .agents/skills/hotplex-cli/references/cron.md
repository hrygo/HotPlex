# Cron operations

Use the `hotplex cron` command family for scheduled agent work. Inspect the
installed validators before creating a job:

    hotplex cron --help
    hotplex cron create --help

Accepted schedule forms are `cron:<expression>`, `every:<duration>`,
`at:<RFC3339>`, and `at:+<duration>`. Use a portable RFC3339 timestamp rather
than shell-specific date arithmetic. Recurring schedules need explicit
lifecycle limits such as `--max-runs` and `--expires-at`.

Use isolated execution by default. Its prompt must contain all context needed
for a later run. For an isolated recurring job, use a self-contained prompt and
explicit lifecycle limits:

    hotplex cron create \
      --name "health-check" \
      --schedule "every:30m" \
      --message "Read the current gateway health and report only actionable failures." \
      --bot-id "<BOT_ID>" \
      --owner-id "<OWNER_ID>" \
      --max-runs 10 \
      --expires-at "2030-01-01T00:00:00Z" \
      --platform cron \
      --silent

Capture the returned identifier, then verify it through the independent JSON
read path without triggering execution:

    hotplex cron get <JOB_ID> --json

For a safe isolated one-shot relative schedule, use the parser-accepted
`at:+duration` form and an explicit cleanup policy:

    hotplex cron create \
      --name "one-shot-health-check" \
      --schedule "at:+10m" \
      --message "Read the current gateway health and report the result." \
      --bot-id "<BOT_ID>" \
      --owner-id "<OWNER_ID>" \
      --platform cron \
      --delete-after-run

Then independently run:

    hotplex cron get <JOB_ID> --json

Use `--attach` only when the request explicitly needs an existing session and
the current command help confirms the attached-session requirements. An
attached job needs `GATEWAY_SESSION_ID`; use `at:+duration` for a safe
one-shot, or `every:<duration>` with explicit recurring limits:

    hotplex cron create \
      --attach \
      --name "attached-health-check" \
      --message "Read the current gateway health." \
      --schedule "at:+10m"

Verify the returned job ID independently:

    hotplex cron get <JOB_ID> --json

Use the present `GATEWAY_*` identity and delivery-routing keys when the
installed command requires them; never print their values or copy unrelated
environment variables into a prompt. Confirm the semantics of `--silent`,
`--delete-after-run`, `--max-retries`, `--timeout`, and `--allowed-tools`
against the current help and validators before using them.

Use `hotplex cron list` to find jobs, `hotplex cron update` to change one,
`hotplex cron trigger` to run one immediately, `hotplex cron history` to
inspect executions, and `hotplex cron delete` only after explicit removal
authorization. A list or history result is not a substitute for the
post-create `cron get --json` verification.
