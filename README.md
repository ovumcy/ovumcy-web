# Ovumcy

[![CI](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml/badge.svg)](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml)
[![CodeQL](https://github.com/terraincognita07/ovumcy/actions/workflows/codeql.yml/badge.svg)](https://github.com/terraincognita07/ovumcy/actions/workflows/codeql.yml)
[![Coverage](https://codecov.io/gh/terraincognita07/ovumcy/graph/badge.svg)](https://codecov.io/gh/terraincognita07/ovumcy)
[![Go Report Card](https://goreportcard.com/badge/github.com/terraincognita07/ovumcy)](https://goreportcard.com/report/github.com/terraincognita07/ovumcy)
[![Release](https://img.shields.io/github/v/release/terraincognita07/ovumcy?display_name=tag)](https://github.com/terraincognita07/ovumcy/releases)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](https://github.com/terraincognita07/ovumcy/pkgs/container/ovumcy)
[![Self-hosted](https://img.shields.io/badge/Self--hosted-yes-2ea44f)](https://github.com/terraincognita07/ovumcy/blob/main/docs/self-hosted.md)
[![No telemetry](https://img.shields.io/badge/Telemetry-none-2ea44f)](https://github.com/terraincognita07/ovumcy#privacy-and-security)

Ovumcy is a privacy-first, self-hosted menstrual cycle tracker.
It is built for people who want fast daily tracking, useful cycle insights, and data that stays under their control.

Ovumcy runs as a single Go service with a server-rendered web UI, can be installed on a phone home screen, and supports SQLite by default with Postgres as an advanced self-hosted path.

This README describes the current `main` branch. The latest tagged release is `v0.3.0`.

## Why Ovumcy Exists

Most cycle tracking apps depend on cloud accounts, analytics, or third-party infrastructure.

Ovumcy is designed as a self-hosted alternative for people who want simple daily tracking, useful cycle insights, and full control over sensitive health data.

## Screenshots

### Get Started Quickly

![Ovumcy registration screen](docs/screenshots/register.jpg)

### Check Today at a Glance

![Ovumcy dashboard screen](docs/screenshots/dashboard.jpg)

### Review the Month

![Ovumcy calendar screen](docs/screenshots/calendar.jpg)

### Use a Comfortable Dark Theme

![Ovumcy dark theme screen](docs/screenshots/dark-theme.jpg)

## Features

- Daily tracking for period days, flow intensity, symptoms, and notes.
- Predictions for next period, ovulation, fertile window, and cycle phase.
- Calendar and statistics views for longer-term pattern spotting.
- Mobile home-screen install support on the current `main` branch.
- CSV and JSON export for backup, portability, and personal review.
- Russian and English localization.
- Self-hosted deployment with Docker or a single Go binary.

## Privacy and Security

- No analytics or ad trackers.
- No third-party API dependencies for core functionality.
- Essential first-party cookies only (auth, CSRF, language, timezone, short-lived flash/recovery state).
- Data stays on infrastructure you control.
- Automated security checks cover CodeQL, gosec, Trivy filesystem/container scans, and CycloneDX SBOM generation in GitHub Actions.
- SQLite is the baseline default; Postgres is available for advanced self-hosted deployments through official example stacks.

If you found a security issue, see [SECURITY.md](SECURITY.md).

## Architecture

```text
Browser / Mobile Home Screen
            |
            v
   Reverse Proxy (optional)
            |
            v
       Ovumcy Server
            |
            v
SQLite (default) / PostgreSQL (advanced)
```

- `Browser UI`: server-rendered HTML with HTMX and Alpine.js, plus mobile home-screen install support.
- `Go application`: a single service that handles routing, templates, i18n, and domain logic.
- `Storage`: SQLite is the baseline default; Postgres is an advanced self-hosted option.
- `Deployment`: one binary or container, typically behind a reverse proxy.

## Tech Stack

- Backend: Go, Fiber, GORM.
- Frontend: server-rendered HTML templates, HTMX, Alpine.js, Tailwind CSS.
- Storage: SQLite (baseline) or Postgres (advanced self-hosted).
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
OVUMCY_IMAGE=ghcr.io/terraincognita07/ovumcy:v0.3.0 docker compose up -d
```

Then open `http://localhost:8080`.

For production-style setups:

- use the dedicated reverse-proxy examples from [docs/self-hosted.md](docs/self-hosted.md) instead of exposing `8080` directly;
- use [docs/examples/postgres/docker-compose.yml](docs/examples/postgres/docker-compose.yml) together with [docs/examples/postgres/.env.example](docs/examples/postgres/.env.example) for the official local/private Postgres path;
- choose one storage engine per deployment, because there is no automatic SQLite-to-Postgres migration tool yet.

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

Most self-hosted setups only need a small set of variables:

```env
TZ=UTC
DEFAULT_LANGUAGE=en
SECRET_KEY=replace_with_at_least_32_random_characters
PORT=8080
COOKIE_SECURE=false

DB_DRIVER=sqlite
DB_PATH=data/ovumcy.db
# DATABASE_URL=postgres://ovumcy:change-me@127.0.0.1:5432/ovumcy?sslmode=disable

TRUST_PROXY_ENABLED=false
PROXY_HEADER=X-Forwarded-For
TRUSTED_PROXIES=127.0.0.1,::1
```

Important notes:

- Always set a strong `SECRET_KEY`.
- Set `COOKIE_SECURE=true` when serving over HTTPS.
- Enable `TRUST_PROXY_ENABLED` only when running behind a trusted reverse proxy.
- SQLite is the supported baseline default; Postgres is an advanced self-hosted path that requires `DATABASE_URL`.
- Keep database storage persistent, whether that is a SQLite volume/bind mount or operator-managed Postgres storage.
- Full deployment, backup, reverse-proxy, and Postgres guidance lives in [docs/self-hosted.md](docs/self-hosted.md).

For deployment paths, reverse-proxy examples, backups, restores, and advanced Postgres setups, see [docs/self-hosted.md](docs/self-hosted.md).

## Development

Common commands from the repository root:

```bash
go test ./...
npm run build
go run ./cmd/ovumcy
```

Project structure:

- `cmd/ovumcy` - application entrypoint and runtime bootstrap
- `internal/api` - HTTP transport, handlers, and response mapping
- `internal/services` - domain logic
- `internal/db` - persistence and migrations
- `web/` - templates, JavaScript, and CSS assets

CI runs staticcheck, `go vet`, tests, and frontend build on pushes and pull requests.
Dedicated security workflows run CodeQL plus `gosec`, Trivy filesystem/container scanning, and publish a CycloneDX image SBOM artifact for each scan run.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

For bugs and feature requests, open a GitHub issue:
- https://github.com/terraincognita07/ovumcy/issues

## Releases

- Latest tagged release: `v0.3.0`.
- This README tracks the current `main` branch.
- Publish release notes via GitHub Releases and keep [CHANGELOG.md](CHANGELOG.md) updated.

## Roadmap

### Next Up

- Custom symptoms: add and hide symptoms beyond built-in defaults.
- Import from other trackers: Clue, Flo CSV import.
- Web Push notifications: period predictions delivered via browser push, no third-party services.
- PDF export for clinical use: printable cycle summary for medical appointments.
- Extended statistics: cycle variability, symptom heatmaps, phase correlations.
- Partner invite via link: simplified partner onboarding without manual account setup.

### Longer Term

- Managed hosting option.
- Optional end-to-end encrypted sync (client-side key, self-hosted or managed).

## License

Ovumcy is licensed under AGPL v3.
See [LICENSE](LICENSE).
