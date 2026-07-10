# Security Invariants

This file is the **repo-visible** mirror of the security-critical invariants the codebase enforces. The full developer-facing context lives in agent-only files that are intentionally gitignored. New contributors who only see the public repository should be able to follow the list below before changing any code that touches auth, cookies, role boundaries, or privacy data.

Every entry has a corresponding test or set of tests in `SECURITY.md → Test Enforcement Matrix`. If you change behaviour around an entry, the test must change in the same commit.

## Layering

- HTTP transport lives in `internal/api`, business logic in `internal/services`, persistence in `internal/db`. Do not access the database from `internal/api` directly.
- Cross-cutting concerns sit in `internal/security`, `internal/httpx`, and `internal/i18n`. Do not duplicate sealed-cookie or AEAD logic outside `internal/security` — the single HKDF + AES-256-GCM primitive (and both purpose label sets) is `SealedCipher` in `internal/security/sealed_cipher.go`.
- Templates live in `internal/templates` and use Go `html/template` auto-escape. Do not introduce `template.HTML(...)` or `template.JS(...)` with owner- or attacker-controlled input (symptom names, notes, display name, …); typed-string casts bypass contextual escaping. Embed such data as plain strings (let `html/template` escape) or via a `<script type="application/json">` island. Regression: `TestTemplateToJSONEscapesAttributeContext`.

## Role and access control

- Every state-mutating `/api/v1/*` endpoint **must** chain `handler.OwnerOnly` after `handler.AuthRequired`, even though `AuthRequired` already rejects unsupported roles via `ErrAuthUnsupportedRole`. The matrix test `TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route` in `internal/api/owner_only_coverage_regression_test.go` walks every registered route and fails when a non-public mutation accepts an unsupported-role auth cookie.
- Only the `RoleOwner` role exists on the web product path — every account is created as `owner`, and `AuthRequired` rejects any non-owner role via `ErrAuthUnsupportedRole`. Do not introduce other roles or user-facing flows for them. An instance may host more than one independent owner (household self-hosting; single-tenant, not multi-tenant SaaS): each is the sole owner of its own data and never sees another's — enforced by the per-`user_id` scoping in the next point, not by capping the account count.
- Every per-user query at the repository layer filters by `user_id`. There are no path parameters carrying numeric user identifiers — handlers always read the current user from `c.Locals("currentUser")`.
- Input DTOs in `internal/api/input_types.go` are strictly bounded. They never expose `Role`, `AuthSessionVersion`, `MustChangePassword`, `TOTPSecret`, or `RecoveryCodeHash` to client-supplied bodies.

## Authentication and sessions

- Auth, recovery, reset, OIDC state, OIDC step-up, OIDC logout bridge, OIDC link-pending, TOTP pending, TOTP setup, register pickup, and flash cookies are sealed with AES-256-GCM under an HKDF-derived key, with the cookie name (or row id, for field encryption) bound to the AEAD AAD. The AEAD core is `SealedCipher` in `internal/security/sealed_cipher.go`; `internal/api/secure_cookie_codec.go` is a thin framing layer owning only the cookie-name→AAD mapping, the `v2` version envelope (`v1` payloads are rejected on read), and base64url transport. Golden fixtures in `internal/api/secure_cookie_codec_golden_test.go` pin the stored format across refactors.
- TOTP secrets are field-encrypted under a distinct HKDF label set and AAD-bound to `users.id`. See `internal/security/field_crypto.go`; golden fixtures in `internal/security/field_crypto_golden_test.go` pin the persisted ciphertext format.
- Operations that rotate a long-lived credential (password change, password reset, recovery-code regeneration, forced operator reset, TOTP enable/disable, clear-data) must bump `users.auth_session_version` in the same atomic database update, invalidating every active auth cookie for that user. The originating device gets a freshly issued cookie inline.
- Auth sessions are **absolute-expiry only by design** — TTL 7d (30d remember-me), no idle/inactivity timeout. Exposure is bounded by the absolute TTL and by instant server-side revocation: every request re-checks `auth_session_version`, so any credential/posture change invalidates the cookie immediately. The absence of an idle timeout is a deliberate trade-off for a personal tracker, not an omission.
- Auth session tokens use `jwt.SigningMethodHS256`. The parser explicitly rejects non-HMAC and `none` algorithms.
- The OIDC sign-in flow is Authorization Code + PKCE (S256) + `response_mode=form_post`. The verifier and `state`/`nonce` live in a single sealed cookie; the callback exempts CSRF (justified by state/nonce/PKCE replacing it). The ID-token allowlist is asymmetric-only (`RS*`, `ES*`, `PS*`, `EdDSA`); `HS*` and `none` are refused even if the provider advertises them. The discovery-supplied `jwks_uri` is origin-pinned to the configured issuer; a cross-origin `jwks_uri` is rejected before any verifier is built.
- Behind a trusted proxy, the edge rate limiters key on the rightmost untrusted `X-Forwarded-For` hop (relative to `TRUSTED_PROXIES`), so a client-spoofed XFF prefix cannot defeat per-IP limits. `PROXY_HEADER` controls fiber's `c.IP()`, which backs only the secondary per-client auth buckets.
- Rate-limit and `AttemptLimiter` state is **in-memory, process-local by design** — correct only under the single-instance self-hosted contract. It is not shared across replicas and resets on restart; horizontal scaling would multiply per-IP budgets and requires an external shared store. Do not rely on limiter state across processes.

## Privacy and PII

- Recovery codes, TOTP secrets, submitted codes, rate-limit identity material, plaintext passwords, and email addresses must not appear in any log output. `SafeRequestLogPath` (in `internal/api/request_logging.go`) masks `:email`, `:id`, `:date`, and opaque tokens in the request path; the companion `SafeLogError` masks emails and opaque tokens in the request log's error field. Both run before any audit emission.
- Auth and settings flash banners must come from sealed cookies or session state, never from URL query parameters. Do not introduce `?error=`, `?status=`, or `?email=` notification sources.
- Public registration (`POST /api/v1/users`) requires explicit consent — the `consent` field must be truthy (`true`/`1`/`yes`/`on`). The browser checkbox lives on `/register`; the backend rejects requests without consent with `auth.error.consent_required`.
- Health data is **owner-only**. The `clear-data` and `delete-account` flows require the current password and bump `auth_session_version`.
- Restoring a JSON export (`POST /api/v1/imports/json`) is **additive and owner-scoped**: it only creates calendar days the account does not already have, never overwrites or deletes existing days, and re-validates every field of the (untrusted) file server-side. Because it destroys nothing, it needs no password re-authentication — but it is state-mutating and therefore owner-only and CSRF-protected.
- Outbound webhook reminders are **owner-scoped egress**: the request-free notify pass delivers each owner's reminder only to that owner's decrypted URL, so one owner's predicted health data never reaches another owner's endpoint. The endpoint is owner-controlled, so the request envelope is hardened rather than the destination blanket-blocked — bounded timeout, no keep-alives, zero redirects, `http`/`https` only, and a size-capped response read. Delivery logs and returns the destination **host only** (never the full URL, path, query, or userinfo, which may embed a notification token), and every payload carries the medical-safety disclaimer with no secret in the body. Owners configure the webhook from the settings page, where the URL is a **write-only secret**: it is encrypted at rest, and once stored it is never rendered back into the page (only a configured/not-configured status plus at most the hostname is shown), so a token embedded in the URL never re-enters transport. A blank URL on save leaves the stored endpoint unchanged; a distinct remove control clears it. See the matrix's *Webhook Notifications (outbound egress)* rows.

## Calendar feed subscription (.ics)

- The read-only `.ics` feed (`GET /calendar/feed/:token.ics`) is authenticated by a **bearer capability token in the URL path** — the single sanctioned exception to *No secret in transport*, because calendar clients send no cookie or auth header. It is a capability token, **not an auth credential**: it authorizes only a read-only `.ics` of that one owner's own predicted cycle events, never account access, settings, or mutation, and it never touches `auth_session_version`.
- The token is **selector + verifier**: the 16-char selector (~80 bits) is a non-secret plaintext UNIQUE-indexed lookup id; the 32-char verifier (~160 bits) is secret and stored **only as a bcrypt hash**. The full subscribe URL is shown to the owner exactly once at generation (like a recovery code) and is never persisted or re-rendered afterward.
- Verification gives **no oracle**: a malformed token, an unknown selector, a wrong verifier, and a disabled feed all return an identical bare `404` (no body, no `Set-Cookie`), timing-equalized by an unconditional dummy bcrypt on the selector-miss path. The selector is compared with `subtle.ConstantTimeCompare` and the verifier with `bcrypt.CompareHashAndPassword`, combined with a bitwise AND so the bcrypt cost is always paid.
- The subscribe URL is a **write-only secret**. Generate and rotate seal the full URL into a one-time AEAD cookie (`ovumcy_calendar_feed`) and redirect to a dedicated reveal page (`GET /settings/calendar-feed`) that clears the cookie and renders the URL exactly once; on any later settings load, refresh, redirect, JSON response, query string, or log it is **never** rendered again. JSON clients receive only a `next_path` to the reveal page, never the URL or token.
- The token is **revocable and rotatable**: `DELETE /api/v1/users/current/calendar-feed` clears both columns (the feed then `404`s) and `POST …/calendar-feed/rotate` mints a new selector+hash so the previous URL dies immediately. Because the token is a long-lived bearer capability that outlives login sessions, it is **force-cleared in the same atomic `auth_session_version` bump** on password reset via recovery code, recovery-code regeneration, forced operator reset, and clear-data; a routine authenticated password change deliberately **keeps** it (the owner rotates manually).
- The feed is hardened in transport: `SafeRequestLogPath` emits the route template `/calendar/feed/:token.ics` verbatim and the raw-path fallback masks an `<opaque-token>.ics` segment, so the token never reaches a log; the endpoint is per-IP rate-limited and served `X-Robots-Tag: noindex`, `Cache-Control: private`. Every `.ics` event carries the medical-safety disclaimer (it is a prediction surface) and a neutral, contentless title, so no cycle-phase detail leaks to anyone who can see the owner's calendar. See the matrix's *Calendar Feed Subscription (.ics)* rows.

## CSRF and CORS

- CSRF middleware is global on every state-changing request. The single exemption is `POST /auth/oidc/callback`, where the OIDC provider cannot present our token and the sealed `state`/`nonce` cover replay protection.
- Global CORS is **disabled**. Do not enable `Access-Control-Allow-Origin: *`.

## Cryptographic baseline

- AEAD: AES-256-GCM (sealed cookies, field encryption), implemented once in `internal/security/sealed_cipher.go`. Key derivation: HKDF-SHA256 with versioned, purpose-specific salt and info labels; the two purposes derive distinct keys, so cross-purpose opens fail (see `SECURITY.md → SECRET_KEY Usage Map`).
- Randomness: `crypto/rand` for nonces, session IDs, OIDC state/nonce, PKCE verifiers, recovery codes. **Never `math/rand` for security-sensitive values.**
- Comparisons: `crypto/subtle.ConstantTimeCompare` for OIDC state, recovery-code hashes (via `bcrypt.CompareHashAndPassword`), TOTP code validation, and password-state fingerprints.
- Passwords are bcrypt cost 12 (a successful login re-hashes any older, lower-cost hash in place without invalidating the session). Minimum length 8 with at least one uppercase, one lowercase, and one digit.

## Content Security Policy

- The CSP shipped from `cmd/ovumcy/main.go` is `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; worker-src 'none'`. Do not introduce `unsafe-inline`, `unsafe-eval`, `data:` outside `img-src`, or `*` in any directive.
- Templates must not contain inline `<script>` blocks, inline event handlers (`onclick=`, `onload=`, …), inline style attributes, or `javascript:` URLs.
- The same `securityHeadersMiddleware` (`cmd/ovumcy/main.go`) sets these companion headers on every response: `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy` (geolocation/camera/microphone/payment/usb/… all `()`), `Cross-Origin-Opener-Policy: same-origin`, `X-Frame-Options: DENY`, and `Strict-Transport-Security` (controlled by `HSTS_ENABLED`, which defaults to the `COOKIE_SECURE` value; set `HSTS_ENABLED=false` to run secure cookies without the 1-year HTTPS pin).

## Medical safety

- Health predictions are estimates, never fact. Every owner-facing prediction surface — dashboard, stats, and calendar — renders a persistent "estimates, not medical advice or a method of contraception" disclaimer alongside the estimate qualifiers.
- A template refactor cannot silently drop it: backend regressions assert the disclaimer on each surface via a stable `data-*` hook plus the exact safety string (`TestDashboardRendersPredictionDisclaimer`, `TestStatsRendersPredictionDisclaimer`, `TestCalendarRendersPredictionDisclaimer`). This is the one place backend tests deliberately pin localized copy, because the wording itself is the invariant.

## GDPR (Art. 6, 9, 13, 15–22, 30, 32, 33)

- The full operator-facing GDPR compliance walkthrough lives in [`docs/gdpr.md`](gdpr.md).
- The mapping from each GDPR Article onto the technical control and its enforcing test is in [`SECURITY.md → GDPR Cross-Reference`](../SECURITY.md#gdpr-cross-reference).

## Backend HTML regression contract

- Backend tests in `internal/api/*_test.go` assert structural contracts only (`id="..."`, `aria-*`, `data-*` hooks, `data-flash-key`, `data-error-key`, `data-explainer-key`). They never assert exact localized UI copy — the rendered phrase is a Playwright concern. The one deliberate exception is the medical-safety disclaimer (see *Medical safety* above), where the exact wording is itself the invariant.
- Service-layer tests own derived values (counts, thresholds, key-selection policy). When a `data-explainer-key` (or similar `data-*-key`) attribute is exposed on a template element, backend assertions read the attribute, not the rendered phrase.

## Migrations

- All schema changes go through `migrations/` with `internal/db/migrations.go` as the single source of truth. Do not call GORM `AutoMigrate` in application boot.
- SQLite is the baseline storage engine. Postgres is the advanced self-hosted path selected with `DB_DRIVER=postgres` and a matching `DATABASE_URL`. Both migration sets share version numbers; SQLite and Postgres migrations live side by side under `migrations/`.

## Deployment

- The runtime container is `FROM scratch`, runs as `USER 10001:10001`, mounts a read-only filesystem with `tmpfs` for `/tmp`, drops all capabilities, and disables new privileges. Health checks use the in-process `ovumcy healthcheck` subcommand. Do not add shell, package manager, or Node.js / Playwright into the runtime image.
- `SECRET_KEY` (or the file behind `SECRET_KEY_FILE`) is the single application-wide secret. It must be at least 32 bytes of cryptographically secure randomness; placeholder values are refused at startup. Store it separately from data backups (`docs/gdpr.md → SECRET_KEY Management`).
- Default `HOST_BIND_ADDRESS` is `127.0.0.1`; public deployments must use the reverse-proxy compose stacks where only the proxy publishes host ports.

## CI

- All GitHub Actions are pinned to immutable commit SHAs. Workflow `permissions:` are minimal; SARIF and Codecov uploads are gated by a fork-PR check.
- Security scans (`gosec`, Trivy filesystem, Trivy image, CycloneDX SBOM) and CodeQL run in dedicated workflows. They target `./cmd/...` and `./internal/...` plus the runtime image; they intentionally exclude `.tmp/` and other ephemeral lab directories.
- **One documented exception to digest-pinning:** `.clusterfuzzlite/Dockerfile`'s `FROM gcr.io/oss-fuzz-base/base-builder-go` is intentionally not digest-pinned. It is an ephemeral fuzzer-build sandbox — never a shipped or scanned runtime image — and ClusterFuzzLite requires the rolling tag so the bundled Go/clang toolchain stays compatible with `go-118-fuzz-build`; a stale pinned digest would eventually break fuzzer builds rather than improve safety. The `go-118-fuzz-build` Go module it installs (no tagged releases) is pinned to a pseudo-version in `.clusterfuzzlite/build.sh`.
