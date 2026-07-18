# Threat Model

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Threat Model

**In scope** — Ovumcy actively defends against:

- Credential stuffing and bot enumeration against `/api/v1/sessions`, `/api/v1/users`, and `/api/v1/password-resets` (rate limits, sealed pickup, bcrypt-timing equalization).
- Replay of captured sealed cookies (server-side single-use for register pickup; session-version checks for `ovumcy_auth`).
- DOM-XSS into HTMX error responses (status-error fragment is parsed with `DOMParser` and rebuilt via `document.createElement` + `textContent`, never `innerHTML`).
- Cross-site form submission (CSRF middleware on every state-changing endpoint; OIDC callback uses provider-issued `state`/`nonce` instead).
- Algorithm-confusion attacks against ID tokens (asymmetric-algorithm allowlist; `HS*` and `none` are rejected even if the provider advertises them).
- Malicious OIDC discovery metadata redirecting the logout flow to attacker-controlled hosts (`end_session_endpoint` host-pinned to the configured issuer).
- Malicious OIDC discovery metadata redirecting the JWKS key fetch to attacker-controlled hosts (`jwks_uri` origin-pinned to the configured issuer, so token-signature keys are only fetched same-origin).
- Malicious OIDC discovery metadata redirecting the code exchange: `token_endpoint` is origin-pinned to the configured issuer the same way, so the server-side POST carrying the client secret and authorization code can only go same-origin.
- Redirect-based escape from those pins: the OIDC HTTP client itself refuses to follow any HTTP redirect that leaves the issuer origin, so the discovery, JWKS, and token-exchange requests (and the client secret and authorization code the exchange carries) cannot be steered off-origin by a redirecting response either.
- Cross-account ciphertext substitution at the database layer (field encryption is AAD-bound to the row id).
- Trivial password reuse (8-character minimum with three required character classes).
- Account takeover via a malicious or sloppy upstream OIDC IdP asserting a verified email already held by a local-auth Ovumcy account — first-time linking is refused without an explicit password confirmation step (`ovumcy_oidc_link_pending` + `/auth/oidc/link-confirm`), and when the target has TOTP enabled the same submission additionally requires a valid 6-digit code. See *OIDC Account Linking*.

**Out of scope** — Ovumcy assumes the operator is trusted, and does not defend against:

- An operator who reads the SQLite file directly, captures `SECRET_KEY`, or inspects process memory. The deployment model is single-tenant self-hosting; in the typical case the operator is the same person as the user.
- Compromise of the user's endpoint (browser malware, OS keyloggers, shoulder surfing).
- TLS downgrade attacks against an operator who runs the app over plain HTTP and ignores the `COOKIE_SECURE` advisory.
- Side-channel attacks against the host CPU (Spectre-class, cache-timing); Ovumcy uses constant-time comparisons where it matters but cannot defend the underlying hardware.
- Simultaneous compromise of `SECRET_KEY` and the OIDC provider's signing key.

Multi-tenant SaaS deployment, organizational identity management, hardware-key support, and audit-log compliance are not goals of this codebase. They are explicitly out of scope for both threat modeling and feature roadmap.
