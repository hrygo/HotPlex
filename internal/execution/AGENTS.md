# Execution Ingress Ledger

## Overview

Durable, content-free input acceptance records used to make ordinary
client-to-worker delivery idempotent. The package owns persistence only;
Gateway owns AEP acknowledgements and Worker dispatch.

## Invariants

- `(session_id, client_message_id)` is unique.
- Only a SHA-256 payload fingerprint is stored, never the input body.
- `accepted` may advance once to `delivered`, `unknown`, or `failed`.
- Terminal states never regress.
- Records left `accepted` across gateway restart become `unknown`; they are not
  automatically redelivered because the Worker may already have accepted them.
- SQLite writes use the shared `sqlutil.WriteMu`; PostgreSQL relies on MVCC.

## Boundaries

- Do not add cross-session scheduling here; that belongs to #851.
- Do not infer execution completion from delivery; terminal turn correlation
  belongs to the runtime events work in #849.
- Do not persist prompts, metadata values, credentials, or raw Worker errors.
