# Admin and audit operations

Admin mutations require explicit Admin authority and the appropriate
authenticated endpoint. Inspect the available command or API surface before
writing:

    hotplex admin --help
    hotplex audit --help

State the target, mutation, and expected side effect before execution. Use
read-only audit or debug views to verify the result. Keep audit records and
error reports free of bearer tokens, cookies, credentials, and unrelated
payloads.
