# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- No auth/session or privacy-boundary contract was weakened in this release; owner-only cycle factors remain sanitized from partner and viewer surfaces.

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

[Unreleased]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.2...HEAD
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
