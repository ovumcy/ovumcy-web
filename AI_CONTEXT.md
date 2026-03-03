# Ovumcy Project Context

## Project Overview

- Project: ovumcy — privacy-critical application that works with sensitive health-related data.
- Goal: clean, maintainable Go backend + web frontend with strong privacy and security guarantees.
- The repository must be treated as production-grade, not as a playground.

## Architecture and Layers

- Backend: Go.
  - Entrypoint in `cmd/`.
  - HTTP/transport layer in `internal/api`.
  - Business logic in `internal/services`.
  - Persistence / DB access in `internal/db`.
  - Domain models in `internal/models`.
  - Cross-cutting concerns in `internal/security`, `internal/i18n`, `internal/templates`.
- Frontend: `web/` (Tailwind CSS, JS, templates).
- Database migrations live in `migrations/`.
- Local data in `.local/` and `data/` is considered sensitive.

## Service Domains

- Auth / session / recovery / reset:
  - `AuthService`, `RegistrationService`, `LoginService`, `PasswordResetService`, and `AttemptLimiter` own authentication and recovery behavior (password rules, reset tokens, recovery codes, onboarding redirects).
  - The `/forgot-password` flow is email-first (email → recovery code), and HTML auth flows rely on flash/cookie/session state for carrying email and recovery codes.
- Days, symptoms, cycle, viewer:
  - `DayService`, `SymptomService`, cycle-related services, and `ViewerService` own day log normalization, symptom rules, cycle calculations, and owner/partner viewer behavior.
- Stats, dashboard, calendar:
  - `StatsService`, `DashboardViewService`, and `CalendarViewService` own stats aggregation, reliability flags, predictions, and calendar day state.
- Settings and notifications:
  - `SettingsService`, `NotificationService`, and `SettingsViewService` own profile/cycle/password/danger operations, settings status classification, and settings view composition.
- Export:
  - `ExportService` owns export range validation, export data composition (logs + symptoms), flow normalization, and JSON/CSV export entries.
- Onboarding and setup:
  - `OnboardingService` and `SetupService` own onboarding step validation, date/cycle bounds, and first-launch/setup decisions.

## Timezone and "today" Rules

- All “today”-based flows (dashboard header, day save/clear, day panel) use the request-local timezone, not server time.
- Request-local timezone is derived from:
  - `X-Ovumcy-Timezone` header (HTMX),
  - `ovumcy_tz` cookie,
  - server fallback location.
- Timezone inputs are validated with `time.LoadLocation`, and new features that rely on “today” are expected to reuse this resolver.
