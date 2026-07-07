# Enterprise OAuth/SSO IAM Integration

HotPlex WebChat supports enterprise SSO through standard OpenID Connect (OIDC).
The flow is OAuth2 Authorization Code with PKCE plus OIDC ID Token validation,
which is the common integration mode for Keycloak, Okta, Microsoft Entra ID,
Google Workspace, Authing, TOPIAM, and most enterprise IAM/IDaaS systems.

HotPlex does not implement SAML or CAS directly. If your IAM is SAML-only or
CAS-only, use the IAM product's OIDC bridge/client feature, or deploy an identity
broker such as Keycloak in front of it.

## Requirements

Your IAM provider must expose:

| Capability | Requirement |
|------------|-------------|
| Discovery | `/.well-known/openid-configuration` under the configured issuer |
| Flow | Authorization Code flow |
| Client type | Confidential web application with `client_id` and `client_secret` |
| PKCE | S256 code challenge support |
| Token | `id_token` returned from the token endpoint |
| Keys | JWKS endpoint for ID Token signature verification |
| Claims | Stable `sub`; optional `preferred_username`, `name`, `email` in ID Token or UserInfo |

## Register HotPlex in the IAM

Create a confidential OIDC client in your IAM console.

Use this redirect URI when `oauth.external_url` is set:

```text
https://hotplex.example.com/api/auth/oauth/<provider-name>/callback
```

If HotPlex is behind a reverse proxy, prefer setting `oauth.external_url` to the
public HTTPS origin. If it is omitted, HotPlex derives the callback origin from
`Forwarded`, `X-Forwarded-Proto`, `X-Forwarded-Host`, and `Host` headers.

Recommended client settings:

| Setting | Value |
|---------|-------|
| Application type | Web / confidential client |
| Grant type | Authorization Code |
| PKCE | Required or allowed, method `S256` |
| Redirect URI | HotPlex callback URI above |
| Scopes | `openid profile email` |
| Front-channel logout | Not required |
| Refresh tokens | Not required |

## Configure HotPlex

Keep secrets out of YAML by referencing environment variables:

```yaml
oauth:
  external_url: "https://hotplex.example.com"
  providers:
    - name: "keycloak"
      display_name: "Company SSO"
      issuer: "https://sso.example.com/realms/main"
      client_id: "${OAUTH_KEYCLOAK_CLIENT_ID}"
      client_secret: "${OAUTH_KEYCLOAK_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
```

Provider names must match `[a-z0-9-]+`. The name appears in URLs and in the
`user_identities.provider` database column, so choose a stable identifier such as
`keycloak`, `okta`, `entra`, or `google`.

## Claim Mapping

By default HotPlex reads these OIDC claims:

| HotPlex field | Default claim |
|---------------|---------------|
| Username | `preferred_username`, falling back to `sub` |
| Display name | `name` |
| Email | `email` |

If your IAM uses custom claims, configure them explicitly:

```yaml
oauth:
  providers:
    - name: "iam"
      issuer: "https://iam.example.com"
      client_id: "${OAUTH_IAM_CLIENT_ID}"
      client_secret: "${OAUTH_IAM_CLIENT_SECRET}"
      username_claim: "uid"
      display_name_claim: "displayName"
      email_claim: "mail"
```

HotPlex maps accounts by `(provider, sub)`. It does not automatically merge SSO
users with password users that share the same email address.

HotPlex verifies the ID Token first. If username, display name, or email are not
present in the ID Token, it calls the OIDC UserInfo endpoint with the access token
and only accepts the response when its `sub` matches the verified ID Token.

## Multiple Providers

You can configure multiple IAM providers. The WebChat login page renders one SSO
button per provider:

```yaml
oauth:
  external_url: "https://hotplex.example.com"
  providers:
    - name: "entra"
      display_name: "Microsoft Entra ID"
      issuer: "https://login.microsoftonline.com/<tenant-id>/v2.0"
      client_id: "${OAUTH_ENTRA_CLIENT_ID}"
      client_secret: "${OAUTH_ENTRA_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
    - name: "google"
      display_name: "Google Workspace"
      issuer: "https://accounts.google.com"
      client_id: "${OAUTH_GOOGLE_CLIENT_ID}"
      client_secret: "${OAUTH_GOOGLE_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
```

Configuration reloads at runtime. Adding, removing, or rotating providers does
not require a gateway restart, but existing browser login attempts may need to be
started again if their provider was changed during the flow.

## Verification

After changing the configuration, open:

```text
https://hotplex.example.com/api/auth/oauth/providers
```

Expected response:

```json
{
  "providers": [
    { "name": "keycloak", "display_name": "Company SSO" }
  ]
}
```

Then open `/login` and confirm the SSO button appears. Clicking it should
redirect to the IAM authorization endpoint with a `redirect_uri` matching the
registered callback URI.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| No SSO button | `GET /api/auth/oauth/providers` returns a provider; WebChat can reach Gateway with CORS enabled |
| IAM rejects redirect URI | `oauth.external_url` matches the public HTTPS origin and provider name |
| `STATE_EXPIRED` | Complete the IAM login within 5 minutes and ensure browser cookies are enabled |
| `CSRF_DETECTED` | Browser must preserve the `oauth_state` cookie during the IAM round trip |
| `CODE_EXCHANGE_FAILED` | Client secret, redirect URI, PKCE policy, or grant type mismatch |
| `ID_TOKEN_INVALID` | Issuer, audience/client ID, JWKS, or clock skew mismatch |
| User has wrong display name/email | Configure `display_name_claim` / `email_claim` for your IAM |
