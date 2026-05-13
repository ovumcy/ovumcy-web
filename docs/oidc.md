# OIDC / SSO Guide

This guide documents Ovumcy's optional OpenID Connect sign-in for self-hosters who already run a central identity provider.

Use this page together with [README.md](../README.md) and [docs/self-hosted.md](self-hosted.md):

- `README.md` stays the short product and configuration overview.
- `docs/self-hosted.md` defines the supported deployment contract.
- this page explains the OIDC-specific operator setup, provider recipes, rollout guidance, and troubleshooting.

## Current Contract

Ovumcy's OIDC support is optional, but the contract is broader than the first hybrid release:

- sign-in uses server-side Authorization Code + PKCE with `response_mode=form_post`;
- `OIDC_LOGIN_MODE=hybrid` keeps local username/password available alongside SSO;
- `OIDC_LOGIN_MODE=oidc_only` removes public local login, register, and forgot-password entry points from the browser UX;
- the first successful OIDC sign-in uses an existing `(issuer, subject)` link when present, otherwise it falls back to a verified email match;
- `OIDC_AUTO_PROVISION=true` may create a new `owner` account only when `REGISTRATION_MODE=open`;
- `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS` can restrict auto-provisioning to a comma-separated domain allowlist;
- auto-provisioned accounts start without a local password or recovery code;
- users who need recovery codes or password-confirmed sensitive actions must set a local password later in `Settings`;
- successful OIDC sign-in still ends with the normal local `ovumcy_auth` cookie, so the rest of the app keeps using the existing session model;
- logout can stay local or redirect to the provider's `end_session_endpoint`, depending on `OIDC_LOGOUT_MODE` and the provider metadata.

## Required Environment

Use the following variables together:

```env
COOKIE_SECURE=true
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://id.example.com
OIDC_CLIENT_ID=ovumcy
OIDC_CLIENT_SECRET=replace_with_a_client_secret
OIDC_REDIRECT_URL=https://ovumcy.example.com/auth/oidc/callback
OIDC_CA_FILE=/run/certs/oidc-provider-ca.pem
OIDC_LOGIN_MODE=hybrid
OIDC_AUTO_PROVISION=false
OIDC_AUTO_PROVISION_ALLOWED_DOMAINS=
OIDC_LOGOUT_MODE=local
OIDC_POST_LOGOUT_REDIRECT_URL=https://ovumcy.example.com/login
```

Notes:

- `COOKIE_SECURE=true` is mandatory when `OIDC_ENABLED=true`.
- `OIDC_REDIRECT_URL` must be an absolute `https://` URL and its path must be exactly `/auth/oidc/callback`.
- `OIDC_ISSUER_URL` must be the issuer URL itself, not a browser login page URL and not a URL with query parameters or fragments.
- `OIDC_CA_FILE` is optional. Use it only when the provider certificate chain is signed by a private or internal CA that the Ovumcy runtime does not already trust.
- `OIDC_LOGIN_MODE` must be `hybrid` or `oidc_only`.
- `OIDC_LOGOUT_MODE` must be `local`, `provider`, or `auto`.
- `OIDC_POST_LOGOUT_REDIRECT_URL`, when set, must be on the same origin as `OIDC_REDIRECT_URL`, and it must not contain query parameters or fragments.
- `OIDC_AUTO_PROVISION=true` requires `REGISTRATION_MODE=open`.
- `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS` is optional. When present, it must be a comma-separated list such as `example.com,staff.example.com`.

## How Sign-In Works

1. The login page shows a `Sign in with SSO` button when `OIDC_ENABLED=true`.
2. Ovumcy starts a server-side Authorization Code flow with PKCE and writes a sealed one-time state cookie containing the OIDC `state`, `nonce`, PKCE verifier, and expiry timestamp.
3. The identity provider authenticates the user and returns the browser to `/auth/oidc/callback` with `response_mode=form_post`.
4. Ovumcy validates the sealed state, exchanges the authorization code for tokens, and verifies the ID token plus `nonce`.
5. If the `(issuer, subject)` identity link already exists, the linked local account is used immediately.
6. Otherwise, Ovumcy checks for a verified email match against an existing local account.
7. If no account exists and auto-provisioning is enabled, Ovumcy can create a new `owner` account, subject to `REGISTRATION_MODE=open` and any configured domain allowlist.
8. Ovumcy finishes sign-in by issuing the normal local `ovumcy_auth` session cookie.

Provider and auth errors are intentionally kept out of query strings and fragments. Browser-facing failures return through the existing flash-based login UX instead.

## How Auto-Provision Works

Auto-provision is intentionally narrow:

- it creates `owner` accounts only;
- it requires a verified email claim;
- it is denied when `REGISTRATION_MODE=closed`;
- it is denied when `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS` is set and the email domain is not on the allowlist;
- it creates the user without a local password or recovery code.

That last point is important. An auto-provisioned OIDC-only account can use the app immediately, but:

- local password recovery is not available yet;
- recovery-code regeneration is not available yet;
- password-confirmed sensitive actions such as `clear data` or `delete account` stay blocked until the user sets a local password in `Settings`.

To enable a local password, OIDC-only users go through a **step-up re-authentication flow**:

1. The user fills the "set local password" form in `Settings`. The browser submits to `POST /api/settings/start-local-password-setup`, which validates the password, prepares its bcrypt hash without touching the database, and redirects the browser to the provider's authorize endpoint with `prompt=login` and `max_age=0` so the provider is forced to re-authenticate the user interactively.
2. The provider posts the result back to the existing `/auth/oidc/callback` endpoint. Ovumcy detects the step-up flow (via a sealed cookie issued in step 1), runs the OIDC code exchange, and requires the resulting ID token's `auth_time` claim (or, if the provider omits it, `iat`) to lie within the last five minutes and the returned `(issuer, subject)` pair to already be linked to the current session's user.
3. Only after both checks succeed does Ovumcy persist the prepared password hash, mint a fresh recovery code, and present it on the dedicated `/recovery-code` page. A stale or mismatched re-auth leaves the account untouched.

The legacy `POST /api/settings/change-password` endpoint still works for accounts that already have local auth enabled (ordinary password rotation). For accounts with `LocalAuthEnabled=false` it now returns `403 oidc reauth required` so the step-up flow above is the only path to enroll a local password.

## How Logout Works

`OIDC_LOGOUT_MODE` controls what happens after Ovumcy clears its own auth cookies:

- `local`: clear Ovumcy cookies only, then return to `/login`;
- `provider`: if the provider session metadata includes `end_session_endpoint`, redirect there with `id_token_hint` and `post_logout_redirect_uri`; otherwise Ovumcy falls back to local logout;
- `auto`: same behavior as `provider`, but intended as the default "best effort" setting for operators who want provider logout when available without breaking logout on providers that do not publish an end-session endpoint.

If you want provider logout, keep `OIDC_POST_LOGOUT_REDIRECT_URL` on the same public origin as the callback URL. If you leave it empty, Ovumcy defaults to your public `/login` URL.

## Provider Recipes

The exact UI labels differ a little by provider version, but the stable requirements are the same:

- callback URL: `https://ovumcy.example.com/auth/oidc/callback`
- client type: confidential web application
- response type / flow: authorization code
- claims: the provider must supply a stable `sub` and a verified email when you want first-login email matching or auto-provisioning

## Provider Compatibility Matrix

The matrix below reflects real interoperability checks under Ovumcy's current hardened browser callback model.

| Provider | Sign-in status | Logout status | Notes |
| --- | --- | --- | --- |
| Keycloak | Live-verified supported | Live-verified provider logout | Full browser sign-in, account resolution, and provider logout were verified end-to-end. |
| authentik | Live-verified supported | Live-verified provider-managed logout | Sign-in is supported. Provider logout may show an authentik-managed invalidation screen before the provider session fully ends. |
| Authelia | Live-verified supported | Local-only fallback | Sign-in is supported. Authelia does not currently expose `end_session_endpoint`, so Ovumcy clears its own session and returns to `/login`. |
| ZITADEL | Supported with the full official deployment | Provider logout depends on the full deployment | Discovery metadata and app setup are compatible, but browser sign-in requires the full ZITADEL deployment that includes the separate Login UI application under `/ui/v2/login`. |
| Dex | Live-verified unsupported | Not applicable | Dex returned `code` and `state` in the callback URL query during live verification, which conflicts with Ovumcy's `form_post`-only HTML callback contract. |
| Pocket ID | Live-verified unsupported | Not applicable | Pocket ID returned `code`, `state`, and issuer data in the callback URL query during live verification, which conflicts with Ovumcy's `form_post`-only HTML callback contract. |

### Why Dex and Pocket ID Are Excluded

Ovumcy intentionally requires `response_mode=form_post` for browser sign-in so auth-sensitive transport data such as `code`, `state`, and provider error details do not appear in user-visible URLs.

During live verification, both Dex and Pocket ID returned callback data in the URL query instead. That behavior conflicts with Ovumcy's hardened callback transport contract, so they are currently treated as unsupported rather than weakening the security model.

### Pocket ID

Pocket ID is currently not supported under Ovumcy's hardened callback model.

Live verification showed Pocket ID returning auth-sensitive callback parameters in the browser URL instead of using `response_mode=form_post`. Until Pocket ID can satisfy the same browser callback contract, do not advertise it as a supported Ovumcy provider.

### Authentik

Recommended setup:

1. Create an Application and an OAuth2/OpenID Provider for Ovumcy.
2. Register the exact Ovumcy callback URL in the provider redirect URI list instead of relying on first-visit auto-save behavior.
3. Use a confidential client with a generated secret.
4. Make sure the provider exposes a verified email claim for users who should sign in.
5. Use the application-specific issuer URL for `OIDC_ISSUER_URL`, not the generic site root.

Recommended Ovumcy mapping:

```env
OIDC_ISSUER_URL=https://authentik.example.com/application/o/ovumcy/
OIDC_CLIENT_ID=ovumcy
OIDC_CLIENT_SECRET=replace_with_a_client_secret
OIDC_REDIRECT_URL=https://ovumcy.example.com/auth/oidc/callback
```

### Dex

Dex is currently not supported under Ovumcy's hardened callback model.

Live verification showed Dex returning `code` and `state` in a query-based callback (`GET /auth/oidc/callback?...`) instead of using `response_mode=form_post`. Ovumcy does not relax its URL-safety contract for that flow, so Dex should be treated as incompatible for now.

### Keycloak

Recommended setup:

1. Create or choose a realm for Ovumcy.
2. Create a new OpenID Connect client for a web application.
3. Keep the authorization code flow enabled and use a confidential client with a secret.
4. Set `Valid Redirect URIs` to the exact Ovumcy callback URL.
5. Use the realm issuer URL for `OIDC_ISSUER_URL`, for example:
   `https://keycloak.example.com/realms/ovumcy`

### Authelia

Authelia configures OIDC clients in its own configuration file. Keep the raw client secret for Ovumcy and the digest only in Authelia's config.

Authelia sign-in is compatible with Ovumcy's callback contract. Logout should still be treated as local-only fallback unless your deployed Authelia build starts publishing a usable `end_session_endpoint`.

Typical client skeleton:

```yaml
identity_providers:
  oidc:
    clients:
      - client_id: "ovumcy"
        client_name: "Ovumcy"
        client_secret: "digest-of-the-raw-client-secret"
        public: false
        redirect_uris:
          - "https://ovumcy.example.com/auth/oidc/callback"
        scopes:
          - "openid"
          - "email"
        response_types:
          - "code"
        grant_types:
          - "authorization_code"
```

### ZITADEL

Recommended setup:

1. Use the full official ZITADEL deployment that includes the separate Login UI application under `/ui/v2/login`.
2. Create a Web Application in your ZITADEL project.
3. Choose the authorization code flow with a client secret.
4. Register the exact Ovumcy callback URL.
5. Use the public issuer root URL for `OIDC_ISSUER_URL`.

Do not treat the minimal API-only compose path as enough for browser sign-in. In practice, ZITADEL's browser login flow depends on the Login UI application being deployed and reachable.

## Rollout Checklist

Before enabling OIDC for real users:

1. Decide whether the instance should start in `hybrid` or `oidc_only`.
   For the first rollout, `hybrid` is safer because local login stays available during testing.
2. Confirm the provider sends the same verified email you expect for your first operator account.
3. Test a first login that creates or links the identity.
4. Test a second login to confirm the stored `(issuer, subject)` link is used.
5. If auto-provisioning is enabled, test one allowed-domain account and one denied-domain account.
6. If auto-provisioned users should be able to recover locally later, test the `Settings -> Set local password` flow and confirm a recovery code is issued.
7. If provider logout is enabled, test both:
   - a provider that exposes `end_session_endpoint`;
   - a fallback case where Ovumcy still clears the local session cleanly.

## Troubleshooting

### The SSO button does not appear

Check:

- `OIDC_ENABLED=true` is present in the running environment;
- startup did not reject another OIDC variable;
- the instance was restarted after changing env;
- `OIDC_LOGIN_MODE` is not mis-typed.

### Startup fails with `OIDC_ENABLED=true requires COOKIE_SECURE=true`

This is expected. OIDC requires secure cookies, so serve Ovumcy over HTTPS and set:

```env
COOKIE_SECURE=true
```

### Startup fails with `OIDC_AUTO_PROVISION=true requires REGISTRATION_MODE=open`

This is expected. Automatic account creation is intentionally coupled to open registration mode so operators do not silently bypass their registration policy.

### Startup fails with an `OIDC_LOGIN_MODE`, `OIDC_LOGOUT_MODE`, or post-logout validation error

Check:

- `OIDC_LOGIN_MODE` is exactly `hybrid` or `oidc_only`;
- `OIDC_LOGOUT_MODE` is exactly `local`, `provider`, or `auto`;
- `OIDC_POST_LOGOUT_REDIRECT_URL`, if set, uses `https://`, stays on the same origin as `OIDC_REDIRECT_URL`, and does not contain query parameters or fragments.

### The provider rejects the callback URL

Typical causes:

- the provider client registration uses a different hostname;
- the provider has `http://` registered but Ovumcy is configured with `https://`;
- the provider has a trailing slash mismatch or a different callback path.

Fix it by making the provider's registered redirect URI exactly match `OIDC_REDIRECT_URL`.

### Clicking SSO returns to `/login` with a generic error

This usually means Ovumcy could not complete provider discovery or token exchange.

Check:

- `OIDC_ISSUER_URL` points to the real issuer, not to a login form URL;
- the provider is reachable from the Ovumcy host or container;
- the provider certificate chain is trusted by the Ovumcy runtime, or `OIDC_CA_FILE` points to a readable PEM bundle for your private CA;
- reverse-proxy DNS and firewall rules allow Ovumcy to reach the provider.

### Auto-provision does not happen

Check:

- `OIDC_AUTO_PROVISION=true`;
- `REGISTRATION_MODE=open`;
- the provider marks the email as verified;
- `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS` is empty, or the email domain is listed there exactly.

### The user can sign in but cannot regenerate a recovery code or use password-confirmed danger-zone actions

This is expected for auto-provisioned OIDC-only accounts until the user sets a local password in `Settings`.

After a local password is set:

- local recovery becomes available;
- recovery-code generation becomes available;
- password-confirmed sensitive actions become available.

### Logout clears Ovumcy but does not sign the user out of the provider

This usually means one of these is true:

- `OIDC_LOGOUT_MODE=local`;
- the provider does not publish `end_session_endpoint` in discovery metadata;
- the current session does not have enough provider logout data for `id_token_hint`.

In that case, Ovumcy still performs a safe local logout and redirects back to `/login`.

### Pocket ID and Dex are rejected even though upstream login succeeds

This is expected under Ovumcy's current browser callback hardening.

Both providers were live-verified returning auth-sensitive callback data in the URL query instead of a `form_post` callback. Ovumcy intentionally refuses that transport shape rather than weakening the URL-safety contract for HTML sign-in.

## Official Provider Documentation

For provider-specific UI details, use the current official docs:

- Authentik OAuth2 / OIDC provider docs: https://docs.goauthentik.io/add-secure-apps/providers/oauth2/
- Keycloak server admin docs: https://www.keycloak.org/docs/latest/server_admin/
- Authelia OIDC client config: https://www.authelia.com/configuration/identity-providers/openid-connect/clients/
- ZITADEL self-hosting and login application docs: https://zitadel.com/docs/self-hosting
- Pocket ID OIDC client authentication: https://pocket-id.org/docs/guides/oidc-client-authentication/ (currently incompatible with Ovumcy's hardened browser callback model)
- Pocket ID client examples and callback/logout fields: https://pocket-id.org/docs/client-examples/outline (currently incompatible with Ovumcy's hardened browser callback model)
- Pocket ID troubleshooting: https://pocket-id.org/docs/troubleshooting/common-issues
- Dex static clients and redirect URIs: https://dexidp.io/docs/connectors/local/ (currently incompatible with Ovumcy's hardened browser callback model)
- Dex scopes and email claims: https://dexidp.io/docs/configuration/custom-scopes-claims-clients/
