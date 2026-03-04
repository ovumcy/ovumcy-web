# Ovumcy

[![CI](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml/badge.svg)](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](https://www.docker.com/)

Ovumcy is a privacy-first, self-hosted menstrual cycle tracker.
It runs as a single Go service with SQLite and a server-rendered web UI.

## Screenshots

### Registration

![Ovumcy registration screen](docs/screenshots/register.jpg)

### Dashboard

![Ovumcy dashboard screen](docs/screenshots/dashboard.jpg)

### Calendar

![Ovumcy calendar screen](docs/screenshots/calendar.jpg)

## Features

- Cycle tracking: period days, flow intensity, symptoms, notes.
- Predictions: next period, ovulation, fertile window.
- Calendar and statistics views.
- Single-user owner workflow (self-hosted private tracking).
- Data export in CSV and JSON.
- Russian and English localization.

## Privacy and Security

- No analytics or ad trackers.
- No third-party API dependencies for core functionality.
- First-party cookies only (auth, CSRF, language).
- Data is stored locally in SQLite on your infrastructure.
- Role model: `owner` has full access.

If you found a security issue, see [SECURITY.md](SECURITY.md).

## Tech Stack

- Backend: Go, Fiber, GORM, SQLite.
- Frontend: server-rendered HTML templates, HTMX, Alpine.js, Tailwind CSS.
- Deployment: Docker or direct binary execution.

## Quick Start

### Docker

Uses the prebuilt image from GHCR by default (`ghcr.io/terraincognita07/ovumcy:latest`).

For public GHCR images, pull does not require GitHub login. `docker compose up -d` is enough because `pull_policy: always` is enabled.

```bash
mkdir -p ovumcy && cd ovumcy
curl -fsSL -o docker-compose.yml https://raw.githubusercontent.com/terraincognita07/ovumcy/main/docker-compose.yml
curl -fsSL -o .env https://raw.githubusercontent.com/terraincognita07/ovumcy/main/.env.example
# edit SECRET_KEY in .env
docker compose up -d
```

Pin a specific image tag if needed:

```bash
OVUMCY_IMAGE=ghcr.io/terraincognita07/ovumcy:v0.1.0 docker compose up -d
```

Then open `http://localhost:8080`.

### Manual

Requirements:

- Go 1.24+
- Node.js 18+

```bash
git clone https://github.com/terraincognita07/ovumcy.git
cd ovumcy
npm ci
npm run build
go run ./cmd/ovumcy
```

## Configuration

Primary variables:

```env
# Core
TZ=UTC
DEFAULT_LANGUAGE=ru
SECRET_KEY=replace_with_at_least_32_random_characters
DB_PATH=data/ovumcy.db
PORT=8080
COOKIE_SECURE=false

# Rate limits
RATE_LIMIT_LOGIN_MAX=8
RATE_LIMIT_LOGIN_WINDOW=15m
RATE_LIMIT_FORGOT_PASSWORD_MAX=8
RATE_LIMIT_FORGOT_PASSWORD_WINDOW=1h
RATE_LIMIT_API_MAX=300
RATE_LIMIT_API_WINDOW=1m

# Reverse proxy trust
TRUST_PROXY_ENABLED=false
PROXY_HEADER=X-Forwarded-For
TRUSTED_PROXIES=127.0.0.1,::1
```

Operational notes:

- Always set a strong `SECRET_KEY`.
- Set `COOKIE_SECURE=true` when serving over HTTPS.
- Enable `TRUST_PROXY_ENABLED` only when running behind a trusted reverse proxy.

## Database and Migrations

- Initial schema is in `migrations/001_init.sql`.
- For post-release schema changes, add forward-only numbered migrations (`002_*.sql`, `003_*.sql`, ...).
- Do not edit already-applied migration files after release.

## Development

Common commands from the repository root:

```bash
go test ./...
npm run build
go run ./cmd/ovumcy
```

CI runs staticcheck, `go vet`, tests, and frontend build on pushes and pull requests.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

For bugs and feature requests, open a GitHub issue:
- https://github.com/terraincognita07/ovumcy/issues

## Releases

- Initial release target: `v0.1.0`.
- Publish release notes via GitHub Releases and keep [CHANGELOG.md](CHANGELOG.md) updated.

## Roadmap

### In Progress

- Mobile PWA: manifest, offline mode, install prompt.
- Reverse proxy examples: Nginx and Caddy configuration samples.

### Next (v0.3)

- Custom symptoms: add and hide symptoms beyond built-in defaults.
- Import from other trackers: Clue, Flo CSV import.
- Web Push notifications: period predictions delivered via browser push, no third-party services.
- PDF export for clinical use: printable cycle summary for medical appointments.
- Extended statistics: cycle variability, symptom heatmaps, phase correlations.
- Partner invite via link: simplified partner onboarding without manual account setup.

### Completed in v0.2.0

- Dark mode with persistent client-side preference and localized toggle labels.
- Playwright smoke coverage for theme persistence across reload and secondary page.
- Register password mismatch UX polish (inline validation before submit, without clearing password fields).

### Considering

- Managed hosting option.
- Optional end-to-end encrypted sync (client-side key, self-hosted or managed).

## License

Ovumcy is licensed under AGPL v3.
See [LICENSE](LICENSE).
