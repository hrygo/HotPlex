# Slack operations

Use this reference only when the user explicitly requests a Slack operation.
Confirm the installed command and required destination before writing:

    hotplex slack --help
    hotplex slack <subcommand> --help

Supported command families include sending or updating messages, uploading or
downloading files, bookmarks, reactions, channels, and scheduled messages.
Choose the narrowest subcommand and preserve the requested channel or message
identity. Ask for a missing destination instead of inferring one.

Treat message sends, uploads, updates, deletions, reactions, and scheduled
messages as external side effects. State the target and side effect before
execution and require the user's explicit request. Use read-only channel or
bookmark inspection when that is sufficient. Redact tokens and private
response metadata from reports.
