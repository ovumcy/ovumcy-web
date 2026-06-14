# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.3.0] - 2026-06-13

A large security-and-quality release: a multi-phase security audit follow-up
across auth, sessions, OIDC, privacy, and accessibility; medically-aligned
cycle prediction; a full ru/es/de/fr localization pass; major frontend
dependency migrations (htmx 2, Tailwind 4); and substantial test, CI, and
build-hardening work. No database migrations; no breaking API changes.

### Security

- **Storage and proxy hardening.** SQLite now boots with foreign-key enforcement and WAL mode; the per-request rate-limit keying behind a trusted proxy was corrected so limits key on the real client, not the proxy hop.
- **Auth, sessions, and OIDC hardening.** Reworked per-IP / per-identity rate-limit key generation; origin-pinned the OIDC discovery and token endpoints against SSRF; consolidated the AEAD sealed-cookie path; threaded request-scoped `context.Context` through the data layer; and folded the reset-token compare-and-swap into `AuthUserRepository`, dropping its dead fallback. Behavior-preserving where it counts, defense-in-depth elsewhere.
- **Robustness & auth hardening (audit phase 3).** Additional hardening of auth and recovery flows, with the security-claim test matrix kept in step.
- **Contract & privacy fixes (audit phase 2).** Auth/settings validation errors stay in flash/session state instead of URL query parameters; registration and recovery wording was made enumeration-safe.
- **CSRF tokens are kept out of GET request URLs.** Token transport moved off the query string so it cannot leak via browser history, logs, or `Referer`.
- **Security-policy & docs corrections** carried through the audit follow-ups (claim matrix, documented headers/rate-limits) to keep `SECURITY.md` true to the code.

### Changed

- **Cycle predictions now use the median cycle length, not the mean**, and the current cycle day is counted on a DST-immune calendar basis. Medical docs and on-screen labeling were aligned to match.
- **Full localization pass across ru/es/de/fr.** Count-bearing stats strings use CLDR plural categories; Russian copy uses the consistent formal «Вы» register; terminology unified (e.g. ru «панель»/«Аналитика»/«Базовая линия»/«БТТ», es/de/fr canon). Walkthrough findings additionally fixed an i18n gap, a settings toggle bug, a dashboard label, and a short-cycle note.
- **Accessibility hardening (audit phase 4).** Focus management/restoration for the confirm modal, `aria-live` status and toast regions, and a skip-to-content link.
- **Operator docs:** documented `REGISTRATION_MODE=closed` as the recommended default for public deployments.

### Dependencies

- **htmx 1.9.12 → 2.0.10** (major) and **Tailwind CSS 3.4.19 → 4.3.0 → 4.3.1** (major) frontend migrations, with the committed `web/static` bundle rebuilt against the new toolchains.
- Go: `golang.org/x/net` 0.55 → 0.56 and the `go-minor-patch` group (6 updates).
- Tooling/base: Alpine 3.22.3 → 3.24.0, the GitHub Actions group (14 updates), `eslint` 10.4.1 → 10.5.0, `globals` 15.15.0 → 17.6.0, and other npm dev-dep minor/patch bumps.

### Internal

- **Hermetic asset builds + CI stale-bundle guard** — committed `web/static` must match a fresh `npm run build`, enforced in CI (audit #2).
- **Refactors:** shared dependency wiring extracted into `internal/bootstrap`; the secure-cookie codec deduplicated via a lazy `Handler.cookieCodec()`; maintainability-debt cleanup (audit phase 6); internal document-collision elimination.
- **Test quality:** a coverage quality pass (dead-symbol removal, vacuous-test fixes, meaningful gap-filling, browser a11y), structural `data-*` test rewrites + TOTP codec coverage, and a round-3 mutation-hardening pass adding per-mutant-verified tests for service-layer survivors introduced by the audit work.
- **CI:** patch-coverage enforced in-CI via `scripts/patchcov`; browser e2e skipped on docs-only pull requests; OpenSSF Scorecard fix — renamed `.mutation/security.md` so it no longer shadows `SECURITY.md` in the basename-matched Security-Policy check.

## [1.2.0] - 2026-06-09

### Added

- **Pregnancy test day field.** An always-shown day field (`none` / `negative` / `positive`) in the dashboard and calendar day editors. A positive test with no later cycle start pauses cycle predictions until a new period is logged. The field is part of the `/api/v1/days` payload (`docs/openapi.yaml`) and the owner CSV and JSON exports (`docs/export.md`); the CSV column is appended at the end so existing column positions stay stable.

### Security

- **All `/api/v1` read endpoints are now owner-gated.** `GET /users/current`, `/days`, `/days/:date`, and `/stats/overview` chain `handler.OwnerOnly` after `AuthRequired`, matching every mutation. Behavior-neutral for the single-role (owner) product — `AuthRequired` already rejects any non-owner role — closing a defense-in-depth uniformity gap.
- **Security documentation corrected to match the code.** Recovery codes are 12 base32-style characters (~60 bits of entropy), not "12 hex / 48 bits"; the documented CSP now includes `manifest-src 'self'`; the `/auth/oidc` rate-limit row and the companion security headers (COOP, X-Frame-Options, nosniff, Referrer-Policy, Permissions-Policy, HSTS) are documented; the web product is clarified as single-role (owner).

### Internal

- Raised `internal/security` OIDC config-validation and `internal/db` repository test coverage (token/state TTL, daily-log read whitelist, symptom owner-scoping).

## [1.1.1] - 2026-06-07

### Security

- **Recovery-code normalization is now rune-safe.** `NormalizeRecoveryCode` used a byte-length check and byte slicing, which produced unstable, non-idempotent output for non-ASCII / invalid-UTF-8 input. Reformatting is now gated on a strict 12-character ASCII-alphanumeric body and is fuzz-guarded. No exploitable bypass existed — downstream validation rejected non-canonical input — but the path is now structurally sound.
- **Go toolchain and dependencies bumped to clear advisories.** Go 1.25.10 → 1.26.4 and `golang.org/x/crypto`, `x/net`, `x/sys`, `x/text` to current releases; `govulncheck` confirms zero reachable vulnerabilities.
- **Release supply-chain verification is documented.** Published images are keyless-signed (cosign), carry SLSA build provenance and an SBOM attestation, and `SECURITY.md` now explains how to verify them with `cosign verify` and `gh attestation verify`.
- Hardened defaults, atomic day writes, UTC cycle math, and flash-message PII removal landed in the unreleased window.

### Changed

- The README now leads with the product story (demo and screenshots above the fold) and a plain-language "How Predictions Work" section; deployment details moved below.
- The cycle-prediction algorithm is fully documented in `docs/cycle-prediction.md`, with worked examples pinned 1:1 by reference tests.
- Test files are excluded from the runtime image build context, `govulncheck` runs as a call-graph reachability gate, and Docker Hub pulls are authenticated to avoid anonymous rate-limit flakiness.

### Internal

- Added mutation testing (gremlins), native fuzzing, property-based and reference-vector tests, cycle-math benchmarks, and a SQLite backup/restore integrity test. Mutation efficacy on `internal/services` is ~94%; see `TESTING.md`.

## [1.1.0] - 2026-05-23

### Security

- **TOTP step-up on `/auth/oidc/link-confirm`.** When the target local-auth account has TOTP enabled, the link-confirmation submission must additionally carry a valid 6-digit `totp_code` form field. The handler runs `TOTPService.CheckRateLimit` and `ValidateCode` with the same per-`(client_ip, user_id)` failure counter and replay rejection (`ErrTOTPReplayed`) as `POST /api/v1/sessions/2fa-challenge`. Previously the handler called `AuthService.AuthenticateCredentials` (password only) and went straight to `setAuthCookie`, allowing an attacker with the victim's password plus a malicious or sloppy upstream IdP to obtain a session and persist a linked OIDC identity without ever holding the second factor.
- **AEAD codec coverage** for `ovumcy_oidc_link_pending` adds the canonical four-invariant lock (round-trip, cross-purpose AAD, tampered byte, rotated key) plus payload expiry and builder field validation. The cookie landed in v1.0.0 without dedicated codec regressions.
- **CSRF route-level locks** for `POST /auth/oidc/link-confirm` and `DELETE /api/v1/days/:date` with the real CSRF middleware enabled, closing the SECURITY.md route-coverage requirement for both endpoints.
- **Cross-user privacy regressions**: Owner B's `GET /api/v1/symptoms` must not surface Owner A's custom symptoms; Owner B's `DELETE /api/v1/days/:date` must not remove Owner A's row on the same calendar day.
- **Sensitive-field leak guard** on `GET /api/v1/users/current` (explicit deny-list assertion on `password_hash`, `recovery_code_hash`, `totp_secret`).

### Fixed

- **`days`** (issue #64): `UpsertDayEntry` was re-applying `DayRange` to a value already canonicalized to UTC-midnight by the caller. For UTC-minus locales `DateAtLocation` shifted the lookup window one day backward, so a second `PUT /api/v1/days/{date}` for the same calendar day missed the existing row and the follow-up `Create` collided with the `uidx_user_date` unique index. Replaced with direct `[dayStart, dayStart+24h)` bounds plus a defensive UTC-midnight normalization at the function entry.

### Changed

- **Link-confirm template** conditionally surfaces a TOTP input when `TOTPRequired=true` (computed in `ShowOIDCLinkConfirmPage` from the target user's `TOTPEnabled`), reusing the `auth.2fa.code_label` / `auth.2fa.code_placeholder` i18n keys.
- **`SECURITY.md`**: new `### OIDC Account Linking` section in the Test Enforcement Matrix links every claim about the link-confirm flow to the specific Go test. Threat-model bullet on malicious-IdP account takeover extended to mention the TOTP gate.
- **`DeleteDay` handler** gains an idempotency lock: `DELETE /api/v1/days/{date}` on a day that has no row returns 204 explicitly.
- **Rate-limit responder coverage** now exercises both the JSON envelope and the HTML fallback path on `RespondAPIRateLimited`.

## [1.0.0] - 2026-05-19

### Changed

- **BREAKING**: The entire HTTP surface moves under `/api/v1/*` and ships as the stable third-party contract. The legacy `/api/*` (non-v1) and the page-route mutators at `/settings/cycle` and `/onboarding/*` are removed.

  Full mapping (canonical REST verbs):

  | Legacy | Canonical `/api/v1/*` |
  | --- | --- |
  | `POST /api/auth/register` | `POST /api/v1/users` |
  | `POST /api/auth/login` | `POST /api/v1/sessions` |
  | `POST /api/auth/logout` | `DELETE /api/v1/sessions/current` |
  | `POST /api/auth/2fa` | `POST /api/v1/sessions/2fa-challenge` |
  | `POST /api/auth/forgot-password` | `POST /api/v1/password-resets` |
  | `POST /api/auth/reset-password` | `POST /api/v1/password-resets/redeem` |
  | `GET /api/days` | `GET /api/v1/days` |
  | `GET /api/days/{date}` | `GET /api/v1/days/{date}` |
  | `GET /api/days/{date}/exists` | `HEAD /api/v1/days/{date}` |
  | `POST /api/days/{date}` (upsert) | `PUT /api/v1/days/{date}` |
  | `DELETE /api/days/{date}` | `DELETE /api/v1/days/{date}` |
  | `POST /api/days/{date}/cycle-start` | `POST /api/v1/days/{date}/cycle-start` |
  | `DELETE /api/log/delete?date=` | `DELETE /api/v1/days?date=` |
  | `GET /api/symptoms` | `GET /api/v1/symptoms` |
  | `POST /api/symptoms` | `POST /api/v1/symptoms` |
  | `POST /api/symptoms/{id}` (update) | `PATCH /api/v1/symptoms/{id}` |
  | `POST /api/symptoms/{id}/archive` & `DELETE /api/symptoms/{id}` | `DELETE /api/v1/symptoms/{id}` (single canonical path) |
  | `POST /api/symptoms/{id}/restore` | `POST /api/v1/symptoms/{id}/restore` |
  | `GET /api/stats/overview` | `GET /api/v1/stats/overview` |
  | `POST /api/export/{summary,csv,json}` | `GET /api/v1/exports/{summary,csv,json}?from=&to=` |
  | `POST /api/settings/profile` | `PATCH /api/v1/users/current/profile` |
  | `POST /api/settings/interface` | `PATCH /api/v1/users/current/interface` |
  | `POST /api/settings/tracking` | `PATCH /api/v1/users/current/tracking` |
  | `POST /settings/cycle` (page route) | `PATCH /api/v1/users/current/cycle` |
  | `POST /api/settings/change-password` | `PUT /api/v1/users/current/password` |
  | `POST /api/settings/start-local-password-setup` | `POST /api/v1/users/current/password/step-up` |
  | `POST /api/settings/regenerate-recovery-code` | `POST /api/v1/users/current/recovery-code` |
  | `POST /api/settings/2fa/verify` | `PUT /api/v1/users/current/2fa` |
  | `POST /api/settings/2fa/disable` | `DELETE /api/v1/users/current/2fa` |
  | `POST /api/settings/clear-data/validate` | `POST /api/v1/users/current/data-wipe/validate` |
  | `POST /api/settings/clear-data` | `POST /api/v1/users/current/data-wipe` |
  | `DELETE /api/settings/delete-account` | `DELETE /api/v1/users/current` |
  | `POST /onboarding/step1` (page route) | `POST /api/v1/onboarding/steps/1` |
  | `POST /onboarding/step2` (page route) | `POST /api/v1/onboarding/steps/2` |
  | `POST /onboarding/complete` (page route) | `POST /api/v1/onboarding/complete` |

### Added

- `GET /api/v1/users/current` ("whoami"): returns the minimum representation of the session subject — id, email, display_name, role, and lifecycle flags (`onboarding_completed`, `local_auth_enabled`, `must_change_password`). Sensitive fields (password/recovery hashes, TOTP secret) are never included.
- Browser regression for the privacy page (`e2e/privacy.spec.ts`) that asserts the rendered copy of every privacy section (zero-collection, your-data, hidden-sections, third-parties, open-source) plus the authenticated breadcrumb/back link contract. The matching backend regression in `internal/api/privacy_route_regressions_test.go` is reduced to structural smoke (section presence and back-link `href` only) so copy edits no longer trigger backend churn.
- GDPR consent control on the public registration form (`/register`). The browser checkbox is wired to a new `consent` field on the registration payload; the backend refuses any registration where `consent` is not truthy and surfaces `auth.error.consent_required` through the same flash/JSON channel used by other auth errors. Localized labels and error copy are added across `en`, `ru`, `es`, `fr`, and `de`. Coverage: `internal/api/auth_register_consent_regression_test.go`.
- Three new privacy-page sections — `data-privacy-section="your-rights"`, `"retention"`, `"predictions"` — render the GDPR Art. 13/15-22 disclosures alongside the existing data-protection summary, with full translations across all five UI locales. Coverage: extended `internal/api/privacy_route_regressions_test.go` plus `e2e/privacy.spec.ts`.
- Forward-looking role-boundary coverage matrix `TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route` in `internal/api/owner_only_coverage_regression_test.go`. The test discovers every registered `/api/v1/*` route through `app.GetRoutes()`, filters out the public auth endpoints, and asserts each remaining route rejects an unsupported-role auth cookie with `403` + a cleared `ovumcy_auth` cookie. New state-mutating endpoints inherit this coverage automatically.
- `docs/gdpr.md` operator-facing GDPR compliance guide. Maps each in-scope GDPR obligation (Art. 6, 9, 13, 15-22, 30, 32, 33) onto the technical control plus operator action (encryption at rest via LUKS/BitLocker, backup hygiene, `SECRET_KEY` separation, DSAR fulfilment through export, audit log retention, breach notification runbook). Referenced from `SECURITY.md → GDPR Cross-Reference`.
- `docs/SECURITY_INVARIANTS.md` — repo-visible mirror of the security-critical invariants previously documented only in agent-only context files. Contributors who only see the public repository can read the layering, role-boundary, AEAD, CSP, GDPR, and CI rules without depending on the gitignored agent context.

### Security

- **Onboarding endpoints now require `handler.OwnerOnly`.** `POST /api/v1/onboarding/steps/1`, `POST /api/v1/onboarding/steps/2`, and `POST /api/v1/onboarding/complete` were previously only guarded by `handler.AuthRequired`, which transitively rejects unsupported roles through `ErrAuthUnsupportedRole`. The explicit `OwnerOnly` middleware closes the defense-in-depth gap so any future regression of `AuthRequired` cannot expose onboarding mutations. Coverage: `TestUnsupportedLegacyRoleOnboardingMutationsAreRejected` in `internal/api/smoke_unsupported_role_flow_test.go` plus the route-discovery matrix above.

### Changed

- Backend HTML regressions for shared explainer/empty-state/warning blocks now assert stable `data-*` hooks instead of exact UI copy. The dashboard, calendar, and stats templates expose `data-explainer-key="<i18n-key>"` (and `data-explainer-primary-key`/`data-explainer-secondary-key` for the multi-line calendar variant) on their prediction-explainer containers, so backend tests verify the policy-selected key against the service-layer contract rather than the localized phrase. Stats empty-state coverage moves to `data-stats-empty-state` + `data-stats-completed-cycles`, dashboard stale-cycle coverage moves to `data-dashboard-cycle-warnings`/`data-dashboard-stale-warning`/`data-dashboard-phase`, and privacy sections expose `data-privacy-section="..."` anchors.
- Flash/error/subtitle regressions on auth and settings pages now assert i18n keys through stable `data-flash-key`/`data-flash-status`/`data-error-key`/`data-subtitle-key` attributes. The settings page renders the success/error banner with `data-flash-key="<key>"` plus a `data-flash-target="change_password"` qualifier for change-password errors; auth pages (login, register, forgot-password, reset-password, 2FA, recovery) carry `data-error-key="<key>"` on every `data-auth-server-error` block; the forgot-password subtitle exposes `data-subtitle-key` plus `data-forgot-step`; the 404 page exposes `data-not-found-title`/`data-title-key`; and the dashboard usage-goal panel exposes `data-usage-goal-label-key`/`data-usage-goal-summary-key`. The shared HTMX status wrapper (`httpx.StatusErrorMarkup`) now accepts an optional error key and emits the same `data-flash-key`/`data-flash-status="error"` attributes so HTMX inline errors share the same contract.
- Frontend npm devDependencies in `package.json` are now exact-pinned (`htmx.org`, `tailwindcss`, `@playwright/test`, `eslint`, `jsdom`, `otplib`, `globals`, `@eslint/js`). Matches the existing `go.mod` exact-pinning baseline and removes the residual supply-chain surface where a `npm install` outside `npm ci` could pull a minor/patch update without a corresponding lockfile bump.

## [0.9.5] - 2026-05-15

### Added
- Runtime proof-of-concept regression tests for the two OIDC contracts hardened in v0.9.5 (`internal/security/oidc_runtime_poc_test.go`). The suite stands up a controlled OIDC provider via `httptest.NewUnstartedServer` with a real TLS leaf signed by a per-test CA, configures `security.OIDCClient` against that issuer through the production `OIDC_CA_FILE` path, and exercises four contracts end-to-end without booting Ovumcy: (1) a malicious `end_session_endpoint` on a different host is stripped from the metadata so logout falls back to local-only, (2) a same-origin `end_session_endpoint` flows through, (3) an ID token signed with HS256 using the JWKS RSA public key as the HMAC secret is refused by the verifier (algorithm-confusion downgrade), and (4) an unsigned `alg=none` token is refused.
- Frontend JavaScript unit-test suite under `web/src/js/__tests__/`, executed via `npm run test:unit` (Node's built-in test runner + jsdom). Twenty-seven tests cover the four security-sensitive client-side surfaces previously only exercised indirectly through Playwright e2e: CSRF token injection on the `htmx:configRequest` hook, the safe-by-construction DOM swap on `htmx:responseError` (the Sprint 3 #9 contract), the `isSafeClientTimezone` validator backing the `ovumcy_tz` cookie write, and the `navigator.clipboard` → `document.execCommand("copy")` fallback used by the recovery-code copy UI. The suite is wired into the CI workflow alongside `lint:js`.
- Subprocess smoke test for the operator CLI (`go test ./cmd/ovumcy -run TestCLISubprocessSmoke`). The previous CLI test surface exercised the dispatch helpers in-process; the new test builds the real `ovumcy` binary into a temp directory and runs `users list`, an intentional usage-error invocation, and a placeholder-secret invocation as subprocesses, so argv parsing, env-var pickup, and exit codes are no longer invisible to the suite. The test is skipped under `go test -short` so the day-to-day suite stays fast and is run by CI without `-short`.

### Security
- `POST /api/settings/clear-data` now bumps `users.auth_session_version` atomically with the data wipe. Any auth cookie that existed before the clear is invalidated on its next request, and the originating device is refreshed inline so the user that triggered the wipe stays signed in. Closes a defense-in-depth gap where a "panic clear" gesture left other sessions authenticated to the freshly-empty account.
- HTMX error responses are no longer assigned through `innerHTML` on the client. The status-error fragment returned by the server is parsed with `DOMParser` and re-built with `document.createElement` + `textContent` before being inserted via `replaceChildren`. Server-rendered error templates already escape user-supplied values, so today this is purely defense-in-depth; any future regression that lets unescaped HTML into an error response would otherwise become an instant DOM-XSS sink.
- Encrypted TOTP secrets are now bound to the owner's user id via AES-GCM additional-authenticated-data (`ovumcy.field.totp_secret:<userID>`). A database-level swap of one user's encrypted secret into another user's row no longer opens under the second user's id, so an attacker with database write privilege cannot pass 2FA for another account by lifting the ciphertext. `DecryptField` keeps a legacy fallback for pre-aad ciphertexts and lazily re-encrypts them under the new aad-bound format on the next successful 2FA login, without bumping `auth_session_version` (the user did not just change their security posture).
- Register-pickup cookie is now single-use server-side. `POST /api/auth/register` persists an opaque nonce in a new `register_pickup_tokens` table, and `GET /register/welcome` atomically consumes it in the same UPDATE. A captured sealed `ovumcy_register_pickup` cookie can no longer be replayed within the 5-minute TTL to mint a second auth session — the second consume returns "already used" and falls through to the same neutral `/login` redirect as a decoy or expired pickup. Migration `022_register_pickup_tokens.sql`.
- 2FA enable and disable now bump `users.auth_session_version` atomically with the TOTP-field update. Every other auth cookie issued before the toggle is invalidated on its next request; the originating session is refreshed inline so the user that performed the toggle stays signed in on their current device. Matches the existing contract for password change, password reset, and recovery-code regeneration.
- OIDC `end_session_endpoint` is now host-pinned to the configured issuer. Discovery metadata that advertises an endpoint on a different host is rejected at provider load time, falling back to local-only logout. Closes a defense-in-depth gap where a compromised or look-alike discovery document could redirect the logout flow (including any `id_token_hint` carried in the URL) to an attacker-controlled host.
- OIDC ID-token verifier now carries an explicit `SupportedSigningAlgs` allowlist (`RS256`, `RS384`, `RS512`, `ES256`, `ES384`, `ES512`, `PS256`, `PS384`, `PS512`, `EdDSA`). Symmetric algorithms and `none` cannot be negotiated even if the upstream provider advertises them, closing an algorithm-confusion downgrade lane.

## [0.9.4] - 2026-05-13

### Added
- TOTP-based two-factor authentication. Owners can enable TOTP 2FA in Settings → Security. Login prompts for the 6-digit TOTP code when 2FA is active. A step-up re-authentication challenge is issued when an OIDC session requires verification.

### Security
- Sealed register pickup cookie closes the per-request Set-Cookie enumeration oracle on `POST /api/auth/register`. The endpoint returns an identical status, body, and single sealed `ovumcy_register_pickup` cookie for both new and duplicate emails; `GET /register/welcome` silently issues a decoy pickup for duplicate addresses and redirects to `/login` with a neutral flash message. The residual two-step timing oracle is documented in `SECURITY.md`.
- TOTP replay protection: step counter validated to reject codes already used within the same 30-second window.
- Login timing side-channel: constant-time bcrypt invocation now applies uniformly to OIDC-only accounts and missing-user paths, preventing user-enumeration via response-time differences.
- OIDC step-up re-authentication: expired OIDC sessions trigger a re-authentication challenge instead of relying solely on the upstream provider's session state.
- Strengthened `Strict-Transport-Security` header with `includeSubDomains` directive.
- Expanded `Permissions-Policy` header to explicitly deny `accelerometer`, `gyroscope`, `payment`, `usb`, `interest-cohort`, and `ambient-light-sensor`.
- Added `Cross-Origin-Opener-Policy: same-origin` to prevent cross-window opener attacks.
- Rate limiting for `/api/auth/register` (8 requests per 15 minutes by default) closes the register enumeration probe surface.
- Per-account rate limit on `/api/auth/logout` (60 requests per 15 minutes by default) to prevent session-disruption attacks.
- Active sessions are atomically revoked when the owner regenerates a recovery code; the originating request receives a fresh auth cookie so the current device stays signed in while all other devices are signed out.

### Fixed
- `DailyLog.Date` and `User.LastPeriodStart` are now canonicalized to UTC midnight on write. A one-time migration backfill corrects existing rows with non-canonical timestamps; observable calendar behavior is unchanged.
- Docker `HEALTHCHECK` no longer relies on `wget`/`curl`, which are absent from the scratch-based runtime image. The binary now ships an `ovumcy healthcheck` subcommand that performs the `/healthz` probe in-process; the `Dockerfile` and all bundled compose examples invoke it directly. Without this fix the container was reported as `unhealthy`.

### Changed
- Updated `github.com/gofiber/fiber/v2` dependency.

## [0.9.3] - 2026-04-30

### Fixed
- Calendar period highlight and dashboard cycle day no longer shift one calendar day earlier for viewers in UTC-minus timezones (e.g. America/Toronto) when daily logs or `users.last_period_start` were persisted with a UTC-based time.Time. Date-only stored values are now read through a new location-agnostic `services.CalendarDay`/`CalendarDayKey` path that takes calendar components from the stored value as-is, instead of running them through `In(location)` which silently moved a UTC-midnight stamp into the previous day in negative-offset locales. Closes #48.

## [0.9.2] - 2026-04-15

### Changed
- Replaced DOM-provided recovery confirmation redirect paths with trusted continue-target tokens, while keeping short-lived recovery cookies backward-compatible during the transition.
- Fixed the Docker image publish workflow parsing failure so the release image pipeline can run again on `main` and on version tags.
- Official compose files, quick-start examples, and README references now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.2`.

### Security
- This patch release hardens the browser recovery-code confirmation sink that CodeQL continued to flag after `v0.9.1` by ensuring the client follows only fixed same-app routes (`/dashboard`, `/onboarding`, `/settings`) selected from trusted tokens rather than DOM text.

## [0.9.1] - 2026-04-15

### Changed
- Tightened the browser recovery-code confirmation flow so client-side continue redirects now allow only the expected same-origin app routes instead of trusting arbitrary DOM-provided paths.
- Reduced helper/test complexity in password-change, recovery transport, migration bootstrap, cycle-hero, and TLS-certificate coverage without changing runtime behavior.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.1`.

### Security
- This patch release closes the CodeQL-reported recovery confirmation redirect sink by forcing recovery continue navigation onto the small allowlisted app-route set (`/dashboard`, `/onboarding`, `/settings`).

## [0.9.0] - 2026-04-15

### Added
- Owner visibility controls for dashboard and calendar entry forms, letting owners hide advanced tracking sections from new entries without removing historical values from private history or exports.
- A segmented dashboard cycle-overview hero with phase cards and browser regressions that keep the hero aligned with calendar predictions and conservative fallback states.

### Changed
- The supported browser product path is now owner-only; legacy non-owner roles are denied before page or API access.
- Recovery-code confirmation, settings, and prediction surfaces were polished to keep clean redirects, localized inline validation, and dashboard/calendar prediction consistency.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.0`, and the README links the public project site at `https://ovumcy.com`.

### Security
- Tracking, export, auth, and recovery flows keep sensitive state out of user-visible URLs while tightening owner-only visibility boundaries.
- The shipped runtime image remains shell-free and package-manager-free, and CI security automation now isolates Codecov OIDC into a least-privilege follow-up job while scanning CI-executed npm dependencies with Trivy.

## [0.8.5] - 2026-03-29

### Changed
- Reduced OIDC-related code complexity without changing runtime behavior by splitting `OIDCConfig.Validate` and `OIDCLoginService.Authenticate` into focused helpers and compacting the OIDC config runtime tests into table-driven coverage.

### Security
- This patch release keeps the hardened OIDC/login/logout contract unchanged while making the security-sensitive validation and linking paths easier to review and maintain.

## [0.8.4] - 2026-03-29

### Changed
- Reissued the patch release on the correct release-packaging commit after the `v0.8.3` tag was created from the previous `main` commit. The runtime feature set is unchanged from the fully green `main` branch.

### Security
- `v0.8.4` is the public patch tag that combines the final CodeQL-driven OIDC helper hardening with the matching release notes and pinned deployment references on the correct tagged commit.

## [0.8.3] - 2026-03-29

### Changed
- Reissued the patch release on the correct release-packaging commit after the `v0.8.2` tag was created from the previous `main` commit. The runtime feature set is unchanged from the fully green `main` branch.

### Security
- `v0.8.3` is the public patch tag that combines the final CodeQL-driven OIDC helper hardening with the matching release notes and pinned deployment references.

## [0.8.2] - 2026-03-29

### Changed
- Removed the last reflected callback-markup pattern from the local OIDC browser-test runtime helper by switching the `form_post` bridge to a constant HTML shell plus a one-time JSON payload endpoint.

### Security
- This patch release supersedes `v0.8.1` for public rollout: it keeps the same OIDC feature set and release packaging, while adding the final CodeQL-driven hardening needed to clear the remaining reflected-XSS alert in the local OIDC test harness.

## [0.8.1] - 2026-03-29

### Changed
- Hardened the local OIDC browser-test runtime helper so it no longer reflects unvalidated transport values, echoes internal error messages in JSON, or accepts arbitrary post-logout redirects.

### Security
- This patch release keeps the `v0.8.0` OIDC feature set but removes the remaining CodeQL warnings from the local OIDC test harness before the public release tag.

## [0.8.0] - 2026-03-29

### Added
- Optional OpenID Connect sign-in for self-hosted deployments, including `hybrid` and `oidc_only` login modes, first login by verified email, stored `(issuer, subject)` links, and operator-facing OIDC documentation.
- OIDC auto-provision for owner accounts when registration is open and the configured allowlist permits the provider email domain.
- Provider logout support through a same-origin bridge together with local-password enablement for OIDC-only accounts.

### Changed
- OIDC provider logout state now stays server-side and is keyed by the auth-session `sid`, which prevents oversized auth headers and keeps raw provider logout parameters out of long-lived cookies.
- Auth and recovery browser coverage now uses cross-browser-portable assertions and validates the full OIDC browser matrix on Chromium, Firefox, and WebKit.

### Security
- Ovumcy keeps the hardened HTML OIDC model: auth/provider-sensitive callback data does not appear in user-visible URLs, and unsupported providers that require query-string callbacks remain excluded from the documented support matrix.

## [0.7.2] - 2026-03-24

### Added
- README now documents how `ovumcy-web`, `ovumcy-app`, and `ovumcy-sync-community` fit together as one product family.

### Changed
- `SECRET_KEY_FILE` now preserves operator-managed path semantics, so absolute secret file paths keep working after the runtime hardening change and startup errors still show the original unreadable path.
- README restores the Go Report Card badge for `github.com/ovumcy/ovumcy-web`.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.7.2`.

### Security
- No auth/session, privacy-boundary, export-data, or role-access contract was weakened in this release.

## [0.7.1] - 2026-03-22

### Added
- First-party French and German UI localization across server-rendered pages, language switching, and onboarding date accessibility labels.
- Supported `SECRET_KEY_FILE` as a file-backed runtime secret source for self-hosted deployments, together with regression coverage and operator-facing documentation.

### Changed
- Public repository, badge, and documentation links now point to `github.com/ovumcy/ovumcy-web`.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.7.1`, matching the post-transfer GHCR namespace for tagged releases.
- CI now treats Codecov upload failures on `push` as non-blocking external errors so downstream smoke lanes still run when Codecov ingest is unavailable.

### Security
- The runtime image and Go toolchain are now pinned to Go `1.25.8`, removing the vulnerable stdlib version previously flagged by Trivy.
- The transitive `flatted` dependency is updated to `3.4.2`.
- No auth/session, privacy-boundary, export-data, or role-access contract was weakened in this release.

## [0.7.0] - 2026-03-16

### Added
- Cross-browser smoke coverage for core owner flows across Chromium, Firefox, and WebKit.
- Additional calendar prediction regressions for the shared facts-only explanation in unpredictable cycle mode.

### Changed
- Prediction explanation copy is now aligned across dashboard, calendar, and stats through one shared owner-only service policy.
- Cycle-factor explanations now stay anchored to the most recent known cycle start so newer onboarding or settings baselines do not get overridden by older manual starts.
- Settings now explain advanced tracking toggles and custom symptom empty, active, and archived states more clearly.
- Local Playwright runs now choose a free app port automatically when no explicit override is provided, which prevents parallel local runs from colliding on a fixed port.
- CI workflows now use Node 24-ready pinned GitHub Actions, while the full browser suite remains on Chromium and the new cross-browser lane stays focused on stable smoke coverage.

### Security
- No auth/session, privacy-boundary, export-data, or prediction-formula contract was weakened in this release.

## [0.6.1] - 2026-03-15

### Changed
- Secure-cookie deployments now emit `Strict-Transport-Security` at the app layer, and self-hosted proxy examples were aligned so they do not add a conflicting second HSTS policy.
- Self-hosted Docker defaults now pin concrete Ovumcy release tags and more specific runtime image versions instead of relying on floating image tags.
- Transport-level API error rendering was tightened by co-locating the shared helper with centralized error mapping and adding a focused regression for JSON, HTMX, and flash redirect branches.
- Spanish navigation and stats labels now use `Análisis` consistently instead of leaving the insights entry in English.

### Security
- Security workflow scanning now uses a digest-pinned Trivy image, and the runtime Dockerfile now ships from Alpine `3.22.3` so the published image no longer carries the vulnerable OpenSSL packages flagged by Trivy.
- No auth/session, privacy-boundary, or export-data contract was weakened in this release.

## [0.6.0] - 2026-03-15

### Added
- Owner-only cycle factor tracking for daily logs, exports, and conservative stats context (`stress`, `illness`, `travel`, `sleep disruption`, `medication changes`).
- A privacy-safe hero demo asset pack, including the mobile install prompt capture contract and refreshed demo documentation.

### Changed
- Stats now stay more conservative with sparse data: basic insights unlock later, reliability messaging is clearer, and early-cycle empty states are simpler.
- Dashboard and settings owner flows were refined to reduce redundancy, improve day logging clarity, and better align destructive copy with actual behavior.
- Irregular-cycle prediction copy now avoids implying a precise ovulation date when recent data is sparse and prefers more cautious wording.
- HTML regression coverage and Codecov publication were tightened so patch-status checks remain reliable in CI.

### Security
- No auth/session or privacy-boundary contract was weakened in this release; owner-only cycle factors remain sanitized outside the supported owner browser path.

## [0.5.0] - 2026-03-15

### Added
- PDF export with embedded fonts for printable cycle summaries alongside the existing CSV and JSON exports.
- Advanced owner tracking controls and richer phase/context insights, including BBT and cervical mucus tracking surfaces.
- Runtime-gated public registration via `REGISTRATION_MODE=open|closed` for operator-restricted self-hosted instances.
- Local operator CLI commands for account audit and removal (`users list`, `users delete <email>`).

### Changed
- Registration now acknowledges recovery codes inline after sign-up, and login/register flows preserve safer client UX without storing passwords in browser storage.
- Auth, logout, and destructive settings flows were hardened with broader cookie cleanup, sanitized request/security logging, and tighter browser/API regressions.
- Dashboard, calendar, onboarding, and settings owner flows were simplified and polished across desktop and mobile, including safer fixed-tabbar spacing and lower-friction daily logging.
- Base self-hosted compose defaults now bind to loopback by default, and operator docs were updated to reflect the local/private baseline versus dedicated public reverse-proxy stacks.
- Browser and backend regression coverage was expanded and refactored around more stable behavior contracts for auth, settings, export, onboarding, and mobile layout flows.

### Security
- Public sign-up can now be disabled without introducing a browser admin surface, reducing exposure for internet-facing operator-managed instances.
- Request and security event logging now avoid raw health-date paths and clear all auth-related cookies consistently on logout and account deletion.

## [0.4.1] - 2026-03-10

### Added
- Full Spanish first-party UI localization alongside English and Russian.
- Localized segmented date fields for onboarding, settings cycle, and export flows so day/month/year labels and picker controls remain accessible across supported locales.

### Changed
- Language switching, locale-aware server/browser date formatting, and related regression coverage were extended to cover Spanish across backend and Playwright checks.
- Chromium-owned native date input labels were replaced in affected flows while preserving the existing ISO `YYYY-MM-DD` transport contract.
- README now documents the supported UI languages and `DEFAULT_LANGUAGE` values for self-hosted operators.

## [0.4.0] - 2026-03-09

### Added
- Owner-managed custom symptom lifecycle with create, rename, hide, and restore flows that preserve historical logs, exports, and stats.
- Focused backend and browser regressions for owner-only symptom routes, archived-symptom behavior, request-local onboarding/settings dates, and simplified settings symptom controls.

### Changed
- Settings and onboarding now keep request-local cycle dates stable through the raw `ovumcy_tz` IANA cookie contract plus an onboarding `client_timezone` fallback.
- Custom symptom validation now blocks duplicate, built-in, markup-like, and over-limit names with row-local HTMX feedback instead of silent failures.
- Settings custom symptom controls were simplified to name-and-icon management; color remains a stored compatibility field with default-on-create and preserve-on-update behavior.
- Danger-zone clear-data flow now removes owner custom symptoms together with daily logs and cycle settings while preserving built-in symptom definitions.
- Settings, dashboard, and calendar symptom UI was tightened to reduce overflow, hide empty custom-symptom groups, and keep compact chips readable.

## [0.3.2] - 2026-03-08

### Changed
- Frontend runtime was prepared for strict CSP by removing Alpine and inline script dependencies from shared templates and client-side flows.
- Default HTTP responses now include a first-party Content-Security-Policy, and HTMX is configured in CSP-safe mode.
- Browser and API regressions were updated to use stable data hooks instead of Alpine-specific selectors and inline state.
- The web app manifest is now served with the correct `application/manifest+json` content type.

## [0.3.1] - 2026-03-07

### Changed
- Rate-limit responses now flow through shared API error mapping instead of hand-rolled middleware transport branches.
- Recovery-code issuance page is now single-view transport and clears its page cookie after the first successful render.
- Auth and recovery regression coverage was updated to keep secrets out of JSON/URLs and to align browser smoke tests with the single-view recovery flow.
- Several API regression tests were simplified to focus on stable outcomes instead of brittle Alpine/HTMX/template wiring details.
- Manual quick-start documentation now includes a PowerShell `SECRET_KEY` example.

## [0.3.0] - 2026-03-07

### Added
- Mobile PWA install support with a web app manifest, home-screen icons, and a shared install prompt for supported mobile browsers.
- Regression coverage for the shared mobile install banner and native install-prompt wiring.
- Baseline browser hardening headers on HTTP responses (`X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, `X-Frame-Options`).

### Changed
- Mobile PWA support is currently install-only; offline mode and service workers remain intentionally deferred pending privacy review.
- Code scanning and security automation were expanded with dedicated CodeQL, gosec, Trivy filesystem/image scans, CycloneDX SBOM generation, and Codecov coverage reporting in CI.
- HTMX not-found responses now flow through centralized error mapping.
- Backend complexity was reduced and regression coverage increased across startup/bootstrap, API regression tests, and cycle/export services.
- Startup logging was hardened to avoid exposing forgot-password rate-limit details.
- README and public project documentation were refreshed to better explain product scope and self-hosted positioning.

## [0.2.5] - 2026-03-07

### Added
- Optional Postgres runtime support for advanced self-hosted deployments.
- Official local/private bundled Postgres compose stack under `docs/examples/postgres/`.
- Official public self-hosted Postgres reverse-proxy examples for Caddy and Nginx.
- Dedicated Postgres browser smoke lane in CI.

### Changed
- Auth/session handling was hardened so sealed auth cookies are enforced and forced password resets revoke stale sessions.
- SQL tracing was hardened to keep bind values out of warn/error logs.
- Self-hosted documentation now covers baseline operations, backup/restore, configuration profiles, and both SQLite and Postgres deployment paths.
- Docker-backed Postgres tests and CI coverage were stabilized for cold GitHub runners.

## [0.2.0] - 2026-03-04

### Added
- Security policy in `SECURITY.md`.
- Contribution guidelines in `CONTRIBUTING.md`.
- Code of conduct in `CODE_OF_CONDUCT.md`.
- Public brand assets (`web/static/brand/*`) and SVG favicon.
- Mobile quick navigation tab bar for faster section switching.
- Dark mode with persistent client-side preference (`ovumcy_theme`) and localized theme toggle labels.
- Playwright smoke coverage for theme persistence across reload and secondary page in one browser context.
- Register page client-validation hooks for password-mismatch UX.

### Changed
- Date validation was hardened in onboarding step 1 and settings cycle start bounds.
- Dashboard cycle-day calculation is now bounded by cycle length, and stale-cycle detection uses owner cycle anchor (`last_period_start`) to avoid misleading stale data.
- Dashboard predictions are projected into upcoming cycles, and stale baseline dates now show explicit warning/unknown states.
- Date formatting is locale-aware in dashboard and settings export summaries (RU/EN consistency).
- Settings cycle warnings now render contextually instead of keeping all variants visible in DOM.
- Settings export range uses native `type="date"` inputs with min/max bounds where supported.
- Calendar opens today's editor by default when `/calendar` has no `day`/`month` query parameters.
- Calendar/day-editor mobile layout was tightened to prevent clipped badges and reduce form footprint on narrow screens.
- Day editor now uses explicit `Save` action; field-change auto-save was removed.
- Symptoms are grouped into logical panels across dashboard and day-editor layouts.
- Stats cards and chart captions now show explicit no-data states; trend/symptom panels reserve stable height on large screens.
- Stats current-phase card follows stale-cycle logic and shows unknown/stale hints when baseline is outdated.
- Profile save supports inline HTMX success feedback; success statuses are dismissible with explicit close controls.
- Desktop nav user block styling was refined: user identity is metadata (not tab-like), logout has clear destructive affordance, and profile-name hinting was simplified.
- Navbar current-user label typography was softened (no all-caps emphasis).
- Light-theme range slider thumbs have improved contrast.
- Register password mismatch now shows inline validation before submit and keeps both password fields intact.
- Privacy breadcrumb naming was aligned with authenticated navigation labels (`Dashboard`/`Панель`).
- Russian copy was polished for consistent use of `надёжный`.
- Language switch active state styling was hardened for mobile with explicit `aria-current` behavior.

## [0.1.0] - 2026-02-23

### Added
- Initial public release of Ovumcy.
- Privacy-first menstrual cycle tracking with:
  - daily logs (period day, flow, symptoms, notes),
  - cycle predictions (next period, ovulation, fertile window),
  - calendar and statistics views,
  - CSV/JSON export,
  - Russian/English localization.

[Unreleased]: https://github.com/ovumcy/ovumcy-web/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/ovumcy/ovumcy-web/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.5...v1.0.0
[0.9.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.4...v0.9.5
[0.9.4]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.3...v0.9.4
[0.9.3]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.5...v0.9.0
[0.8.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.4...v0.8.5
[0.8.4]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.3...v0.8.4
[0.8.3]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.2...v0.8.0
[0.7.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.2.5...v0.3.0
[0.2.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.2.0...v0.2.5
[0.2.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/ovumcy/ovumcy-web/releases/tag/v0.1.0
