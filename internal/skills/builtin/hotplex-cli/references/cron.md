# Cron operations

Use the hotplex cron command family for scheduled agent work. Inspect the
installed validators before creating a job:

    hotplex cron --help
    hotplex cron create --help

Accepted schedule forms are cron:<expression>, every:<duration>,
at:<RFC3339>, and at:+<duration>. Use a portable RFC3339 timestamp instead of
shell-specific date arithmetic. Recurring schedules must include the
lifecycle limit enforced by the installed binary, such as --max-runs or
--expires-at.

Use isolated execution by default. Its prompt must contain all context needed
for a later run. Use --attach only when the request explicitly needs an
existing session and the current command help confirms the attached-session
requirements. Use the present GATEWAY_* identity and delivery-routing keys
when the installed command requires them; never print their values or copy
unrelated environment variables into a prompt.

Create the smallest explicit job with the requested destination, prompt,
schedule, timezone, and owner fields. Confirm the semantics of --silent,
--delete-after-run, retries, timeout, and --allowed-tools against the current
help and validators before using them. Do not infer an identifier or silently
choose a production destination.

For an isolated recurring job, use a self-contained prompt and explicit
lifecycle limits:

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

Capture the returned identifier, then verify it through the independent read
path without triggering execution:

    hotplex cron get <JOB_ID>

For a safe isolated one-shot relative schedule, use the parser-accepted
at:+duration form and an explicit cleanup policy:

    hotplex cron create \
      --name "one-shot-health-check" \
      --schedule "at:+10m" \
      --message "Read the current gateway health and report the result." \
      --bot-id "<BOT_ID>" \
      --owner-id "<OWNER_ID>" \
      --platform cron \
      --delete-after-run

After creation, independently run hotplex cron get with the returned
identifier and report the verified schedule and state. Use hotplex cron list to
find jobs, hotplex cron update to change one, hotplex cron trigger to run one
immediately, hotplex cron history to inspect executions, and hotplex cron
delete only after explicit removal authorization.
