# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Mobile PWA install support with a web app manifest, home-screen icons, and a shared install prompt for supported mobile browsers.
- Regression coverage for the shared mobile install banner and native install-prompt wiring.

### Changed
- Mobile PWA support is currently install-only; offline mode and service workers remain intentionally deferred pending privacy review.

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

[Unreleased]: https://github.com/terraincognita07/ovumcy/compare/v0.2.5...HEAD
[0.2.5]: https://github.com/terraincognita07/ovumcy/compare/v0.2.0...v0.2.5
[0.2.0]: https://github.com/terraincognita07/ovumcy/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/terraincognita07/ovumcy/releases/tag/v0.1.0
