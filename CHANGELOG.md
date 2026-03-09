# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/terraincognita07/ovumcy/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/terraincognita07/ovumcy/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/terraincognita07/ovumcy/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/terraincognita07/ovumcy/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/terraincognita07/ovumcy/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/terraincognita07/ovumcy/compare/v0.2.5...v0.3.0
[0.2.5]: https://github.com/terraincognita07/ovumcy/compare/v0.2.0...v0.2.5
[0.2.0]: https://github.com/terraincognita07/ovumcy/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/terraincognita07/ovumcy/releases/tag/v0.1.0
