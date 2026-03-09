# Ovumcy AI Context: Architecture

## Backend Layers

- Backend: Go.
- Entrypoint in `cmd/`.
- HTTP/transport layer in `internal/api`.
- Business logic in `internal/services`.
- Persistence / DB access in `internal/db`.
- Domain models in `internal/models`.
- Cross-cutting concerns in `internal/security`, `internal/i18n`, and `internal/templates`.
- Shared HTMX status markup wrappers (error and dismissible success) live in `internal/httpx` and are consumed by `internal/api` handlers.
- Entrypoint bootstrap should keep env parsing, runtime config resolution, and Fiber wiring in small helpers so startup behavior can be unit-tested without launching the full app.

## Service Domains

- Auth / session / recovery / reset:
  - `AuthService`, `RegistrationService`, `LoginService`, `PasswordResetService`, and `AttemptLimiter` own authentication and recovery behavior (password rules, reset tokens, recovery codes, onboarding redirects).
  - The `/forgot-password` flow is email-first (email -> recovery code), and HTML auth flows rely on flash/cookie/session state for carrying email and recovery codes.
- Days, symptoms, cycle, viewer:
  - `DayService`, `SymptomService`, cycle-related services, and `ViewerService` own day log normalization, symptom rules, cycle calculations, and owner/partner viewer behavior.
  - Hiding a custom symptom archives it from new-entry pickers but must preserve past daily logs, exports, and stats. Existing entries may still render archived selected symptoms so historical records remain editable and understandable.
  - Custom symptom names are short labels capped at 40 characters. Longer context belongs in day notes, not in symptom labels.
  - The owner-facing custom symptom settings UI uses only name and icon. Color remains a stored compatibility field and must default on create and be preserved on update when omitted by HTML flows.
- Stats, dashboard, calendar:
  - `StatsService`, `DashboardViewService`, and `CalendarViewService` own stats aggregation, reliability flags, predictions, and calendar day state.
- Settings and notifications:
  - `SettingsService`, `NotificationService`, and `SettingsViewService` own profile/cycle/password/danger operations, settings status classification, and settings view composition.
  - Settings notifications for HTML pages are flash/session-only; query parameters (`status`, `success`, `error`) are not valid notification sources.
  - Settings symptom mutations should preserve row-local HTMX success and error feedback so owners do not have to rely on top-of-page banners after editing, hiding, or restoring a custom symptom.
  - "Clear all data" removes owner custom symptoms together with daily logs and cycle settings, but preserves built-in symptom definitions.
- Export:
  - `ExportService` owns export range validation, export data composition (logs + symptoms), flow normalization, and JSON/CSV export entries.
- Onboarding and setup:
  - `OnboardingService` and `SetupService` own onboarding step validation, date/cycle bounds, and first-launch/setup decisions.
  - Onboarding step and completion forms carry a `client_timezone` fallback field so request-local calendar dates remain stable even before the timezone cookie has been established for the browser session.

See `.agents/context/security.md` for auth, cookie, recovery-code, browser-hardening, and logging invariants that apply to these domains.

## Frontend and Templates

- Frontend lives in `web/` and uses Tailwind CSS, JS, and templates.
- Supported first-party UI locales are `en`, `ru`, and `es`. The language switcher must be driven from `internal/i18n` supported locales rather than hardcoded locale lists in templates or JS.
- Do not rely on browser-native accessibility labels of `input[type="date"]` for localized UX in Chromium; use the shared segmented date-field pattern when translated day/month/year labels or picker buttons are required.
- Mobile PWA support is currently limited to manifest + install prompt UX. Do not introduce service workers, offline caches, or cached HTML/data shells without explicit privacy review and dedicated tests.
- UI appearance preference (`light`/`dark`) is a client-only concern stored in `localStorage` (`ovumcy_theme`); base layout should set `html[data-theme]` before CSS loads to avoid theme flash.
- Register page should perform client-side password mismatch validation to avoid wiping both password fields on roundtrip; server-side validation remains authoritative.

## Package Boundaries

- Database migrations live in `migrations/`.
