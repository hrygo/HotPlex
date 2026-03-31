# Security Policy

HotPlex is committed to the security of its users and considers security a high priority.

## Supported Versions

Only the latest `master` and official releases are supported with security updates.

| Version | Supported          |
| ------- | ------------------ |
| v1.0.x  | :white_check_mark: |
| < v1.0  | :x:                |

## Reporting a Vulnerability

If you discover a potential security vulnerability in HotPlex, please do **not** open a public issue. Instead, report it privately through one of the following channels:

1.  **Email**: Send a report to `security@hotplex.io` (or the project maintainers).
2.  **GitHub Private Vulnerability Reporting**: Use the "Report a vulnerability" button on the GitHub Security tab if enabled.

Please include the following in your report:

-   A description of the vulnerability.
-   Steps to reproduce (a proof-of-concept script).
-   The potential impact of the vulnerability.

We will acknowledge receipt of your report within **48 hours** and provide a timeline for addressing the issue. Once the vulnerability is patched, we will issue a security advisory and credit the researcher (unless you prefer to remain anonymous).

## Security Best Practices

### Production Hardening

-   **TLS**: Always enable TLS (`security.tls_enabled: true`) in production.
-   **Authentication**: Configure strong `APIKeys` and `Admin.Tokens`.
-   **JWT**: Use the `JWTValidator` for user-level session authentication.
-   **PGID Isolation**: Ensure the gateway process has sufficient permissions to manage process groups (PGID).

---

*Thank you for helping us keep HotPlex secure!*
