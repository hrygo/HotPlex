# Public HotPlex CLI surface

Generated from the public Cobra command tree. Use installed command help as the final authority for syntax, defaults, and availability.

## hotplex
HotPlex Worker Gateway

## hotplex admin
用户与账号管理（bootstrap admin 等）

Options: --config <string>

## hotplex admin create
创建账号（首个 admin，或后续用户）

Options: --admin --config <string> --password <string> --username <string>

## hotplex audit
Audit log chain operations

## hotplex audit rebase
Re-anchor the audit hash chain at a surviving row (repair)

Options: --config <string> --confirm --next-id <int64>

## hotplex audit verify
Verify the audit hash chain integrity (read-only)

Options: --config <string>

## hotplex config
Manage configuration

## hotplex config validate
Validate configuration file

Options: --config <string>

## hotplex cron
Cron job management

## hotplex cron create
Create a cron job

Options: --allowed-tools <string> --attach --bot-id <string> --bot-name <string> --config <string> --delete-after-run --description <string> --expires-at <string> --max-retries <int> --max-runs <int> --message <string> --name <string> --owner-id <string> --platform <string> --platform-key <string> --schedule <string> --silent --timeout <int> --work-dir <string> --worker-type <string>

## hotplex cron delete
Delete a cron job

Options: --config <string>

## hotplex cron get
Get cron job details

Options: --config <string> --json

## hotplex cron history
Show execution history for a cron job

Options: --config <string> --json

## hotplex cron list
List cron jobs

Options: --config <string> --enabled --json

## hotplex cron trigger
Trigger a cron job execution

Options: --config <string>

## hotplex cron update
Update a cron job

Options: --allowed-tools <string> --bot-id <string> --bot-name <string> --config <string> --delete-after-run --description <string> --enabled --expires-at <string> --max-retries <int> --max-runs <int> --message <string> --owner-id <string> --schedule <string> --silent --timeout <int> --work-dir <string> --worker-type <string>

## hotplex dev
Quick start in development mode

Options: --config <string>

## hotplex doctor
Run diagnostic checks

Options: --category <string> --config <string> --fix --json --verbose

## hotplex gateway
Manage the gateway server

## hotplex gateway restart
Restart the gateway server

Options: --config <string> --daemon --detached --dev

## hotplex gateway start
Start the gateway server

Options: --config <string> --daemon --dev

## hotplex gateway stop
Stop the running gateway server

## hotplex install
Install hotplex binary to PATH

Options: --force --path <string>

## hotplex onboard
Interactive configuration wizard

Options: --config <string> --enable-feishu --enable-slack --feishu-allow-from <stringSlice> --feishu-dm-policy <string> --feishu-group-policy <string> --force --install-service --non-interactive --service-level <string> --slack-allow-from <stringSlice> --slack-dm-policy <string> --slack-group-policy <string>

## hotplex runtime
Runtime operations: inspect and resolve fenced executions

## hotplex runtime fences
List and decide fenced executions (via Admin API)

## hotplex runtime fences abandon
Abandon a fenced execution: fail it with OPERATOR_ABANDONED and unblock the session

Options: --config <string> --confirm --evidence-ref <string> --fence-version <int64> --reason <string>

## hotplex runtime fences list
List fenced executions blocking fresh input

Options: --config <string> --json --limit <int> --session-id <string>

## hotplex runtime fences resolve
Resolve a fence: clear it, keep runtime=unknown, and unblock the session

Options: --config <string> --confirm --evidence-ref <string> --fence-version <int64> --reason <string>

## hotplex security
Run security audit

Options: --config <string> --fix --json --verbose

## hotplex service
Manage system service

## hotplex service install
Install as system service

Options: --config <string> --level <string>

## hotplex service logs
View service logs

Options: --follow --level <string> --lines <int>

## hotplex service restart
Restart system service

Options: --level <string>

## hotplex service start
Start system service

Options: --level <string>

## hotplex service status
Check service status

Options: --json --level <string>

## hotplex service stop
Stop system service

Options: --level <string>

## hotplex service uninstall
Uninstall system service

Options: --level <string>

## hotplex slack
Slack messaging operations

## hotplex slack bookmark
Manage channel bookmarks

## hotplex slack bookmark add
Add a bookmark

Options: --channel <string> --config <string> --emoji <string> --json --title <string> --url <string>

## hotplex slack bookmark list
List bookmarks

Options: --channel <string> --config <string> --json

## hotplex slack bookmark remove
Remove a bookmark

Options: --bookmark-id <string> --channel <string> --config <string>

## hotplex slack delete-file
Delete a file from Slack

Options: --config <string> --file-id <string>

## hotplex slack download-file
Download a file from Slack

Options: --config <string> --file-id <string> --output <string>

## hotplex slack list-channels
List channels and DMs

Options: --config <string> --json --limit <int> --types <string>

## hotplex slack react
Add or remove emoji reactions

## hotplex slack react add
Add a reaction

Options: --channel <string> --config <string> --emoji <string> --ts <string>

## hotplex slack react remove
Remove a reaction

Options: --channel <string> --config <string> --emoji <string> --ts <string>

## hotplex slack schedule-message
Schedule a message for future delivery

Options: --at <string> --channel <string> --config <string> --json --text <string>

## hotplex slack send-message
Send a text message

Options: --channel <string> --config <string> --json --text <string> --thread-ts <string>

## hotplex slack update-message
Update an existing message

Options: --channel <string> --config <string> --json --text <string> --ts <string>

## hotplex slack upload-file
Upload a file to Slack

Options: --channel <string> --comment <string> --config <string> --file <string> --json --max-size <int64> --thread-ts <string> --title <string>

## hotplex status
Check gateway status

Options: --config <string> --format <string>

## hotplex update
Update hotplex to the latest version

Options: --check --restart --yes

## hotplex version
Print version information

Options: --format <string>
