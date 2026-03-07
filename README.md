# Ovumcy

[![CI](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml/badge.svg)](https://github.com/terraincognita07/ovumcy/actions/workflows/ci.yml)
[![CodeQL](https://github.com/terraincognita07/ovumcy/actions/workflows/codeql.yml/badge.svg)](https://github.com/terraincognita07/ovumcy/actions/workflows/codeql.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](https://www.docker.com/)

Ovumcy is a privacy-first, self-hosted menstrual cycle tracker.
It runs as a single Go service with a server-rendered web UI. SQLite is the baseline default; Postgres is an advanced self-hosted path.

## Screenshots

### Registration

![Ovumcy registration screen](docs/screenshots/register.jpg)

### Dashboard

![Ovumcy dashboard screen](docs/screenshots/dashboard.jpg)

### Calendar

![Ovumcy calendar screen](docs/screenshots/calendar.jpg)

### Dark Theme

![Ovumcy dark theme screen](docs/screenshots/dark-theme.jpg)

## Features

- Cycle tracking: period days, flow intensity, symptoms, notes.
- Predictions: next period, ovulation, fertile window.
- Calendar and statistics views.
- Installable mobile app shell with manifest, home-screen icons, and install prompt support.
- Single-user owner workflow (self-hosted private tracking).
- Data export in CSV and JSON.
- Russian and English localization.

## Privacy and Security

- No analytics or ad trackers.
- No third-party API dependencies for core functionality.
- First-party cookies only (auth, CSRF, language).
- Automated security checks cover CodeQL, gosec, Trivy filesystem/container scans, and CycloneDX SBOM artifact generation in GitHub Actions.
- Data stays on infrastructure you control. SQLite is the baseline default; advanced self-hosted deployments can use Postgres through the official bundled and reverse-proxy example stacks.
- Role model: `owner` has full access.

If you found a security issue, see [SECURITY.md](SECURITY.md).

## Tech Stack

- Backend: Go, Fiber, GORM, SQLite (baseline) or Postgres (advanced self-hosted).
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
OVUMCY_IMAGE=ghcr.io/terraincognita07/ovumcy:v0.2.5 docker compose up -d
```

Then open `http://localhost:8080`.
For a public HTTPS deployment, use the dedicated reverse-proxy example stacks from the self-hosted guide instead of exposing `8080` directly.
For an official advanced self-hosted Postgres stack, use [docs/examples/postgres/docker-compose.yml](docs/examples/postgres/docker-compose.yml) together with [docs/examples/postgres/.env.example](docs/examples/postgres/.env.example).
For public self-hosted HTTPS with Postgres, use the dedicated Postgres reverse-proxy stacks from the self-hosted guide instead of combining the baseline proxy examples with Postgres by hand.

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
DEFAULT_LANGUAGE=en
DB_DRIVER=sqlite
SECRET_KEY=replace_with_at_least_32_random_characters
DB_PATH=data/ovumcy.db
# DATABASE_URL=postgres://ovumcy:change-me@127.0.0.1:5432/ovumcy?sslmode=disable
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
- `.env.example` defaults target the local/private base compose path, not the public internet profile.
- `DB_DRIVER=sqlite` is the supported baseline default; `DB_DRIVER=postgres` is an advanced self-hosted path and requires `DATABASE_URL`.
- For a turnkey local/private Postgres deployment, use the dedicated bundled stack under `docs/examples/postgres/` instead of grafting Postgres onto the baseline SQLite compose file by hand.
- No automatic SQLite-to-Postgres migration tool is included in `v0.2.5`; choose one engine per deployment.
- Set `COOKIE_SECURE=true` when serving over HTTPS.
- Enable `TRUST_PROXY_ENABLED` only when running behind a trusted reverse proxy.
- Do not expose Ovumcy's plain HTTP port directly to the public internet.
- Keep the SQLite database on a persistent Docker volume or bind mount, or use operator-managed persistent Postgres storage when `DB_DRIVER=postgres`.

## Database and Migrations

- Initial schema is in `migrations/001_init.sql`.
- For post-release schema changes, add forward-only numbered migrations (`002_*.sql`, `003_*.sql`, ...).
- Do not edit already-applied migration files after release.

## Self-Hosted Operations

The supported self-hosted production baseline is:

- one Ovumcy instance per private deployment;
- a persistent SQLite volume;
- a dedicated reverse proxy at the edge;
- `COOKIE_SECURE=true` under HTTPS;
- `TRUST_PROXY_ENABLED=true` only behind your own trusted proxy;
- no direct public publish of the plain HTTP app port;
- a strong private `SECRET_KEY`.

Before exposing Ovumcy publicly:

1. Generate and store a strong `SECRET_KEY`.
2. Confirm database persistence is backed by a Docker volume or bind mount.
3. Enable HTTPS and set `COOKIE_SECURE=true`.
4. Enable reverse proxy trust only when you control the proxy and have set exact `TRUSTED_PROXIES`.
5. Prefer a reverse-proxy stack where the app service has no published host port at all. If you deviate, keep the plain HTTP app port internal-only.
6. Verify the container becomes healthy after startup.

Routine upgrade flow:

1. Back up the database before upgrading, using the documented self-hosted backup flow.
2. Pull the target image tag and restart the service.
3. Confirm `docker compose ps` shows the container healthy.
4. For the public reverse-proxy stacks, confirm the app still responds through the proxy URL. For the local/private base compose path, confirm `curl -fsS http://127.0.0.1:8080/healthz` succeeds.
5. Roll back to the previous image tag if the new version does not start cleanly.

See [Self-Hosted Operations Guide](docs/self-hosted.md) for the full baseline, manual backup/restore flow, reverse proxy examples, troubleshooting guidance, and upgrade path.

The same guide also documents:

- required vs recommended vs advanced configuration;
- what privacy/security guarantees come from Ovumcy itself;
- what operational safeguards the self-hoster must still provide.
- how to evolve from the baseline path into a stricter advanced self-hosted operating model.
- how to choose Postgres as the runtime for advanced self-hosted deployments.
- how to run the official local/private bundled Postgres stack for advanced self-hosted deployments.
- how to run the official public self-hosted Postgres reverse-proxy stacks for advanced deployments that need both HTTPS and Postgres.

## Development

Common commands from the repository root:

```bash
go test ./...
npm run build
go run ./cmd/ovumcy
```

CI runs staticcheck, `go vet`, tests, and frontend build on pushes and pull requests.
Dedicated security workflows run CodeQL plus `gosec`, Trivy filesystem/container scanning, and publish a CycloneDX image SBOM artifact for each scan run.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

For bugs and feature requests, open a GitHub issue:
- https://github.com/terraincognita07/ovumcy/issues

## Releases

- Current release: `v0.2.5`.
- Publish release notes via GitHub Releases and keep [CHANGELOG.md](CHANGELOG.md) updated.

## Roadmap

### In Progress

- Mobile PWA offline mode only after dedicated privacy review for service-worker scope and cached health data.

### Recently Completed (Unreleased)

- Mobile PWA install support: manifest, home-screen icons, and install prompt without offline caching.

### Next (v0.3)

- Custom symptoms: add and hide symptoms beyond built-in defaults.
- Import from other trackers: Clue, Flo CSV import.
- Web Push notifications: period predictions delivered via browser push, no third-party services.
- PDF export for clinical use: printable cycle summary for medical appointments.
- Extended statistics: cycle variability, symptom heatmaps, phase correlations.
- Partner invite via link: simplified partner onboarding without manual account setup.

### Completed in v0.2.5

- Optional Postgres runtime support for advanced self-hosted deployments.
- Official bundled local/private Postgres compose stack.
- Official public self-hosted Postgres reverse-proxy examples for Caddy and Nginx.
- Auth/session hardening for sealed cookies, forced-reset session invalidation, and privacy-safe SQL logging.
- Stronger self-hosted operations guidance for backup/restore, config profiles, and advanced deployment paths.
- Dedicated Postgres browser smoke lane in CI plus more stable Docker-backed Postgres test startup.

### Completed in v0.2.0

- Dark mode with persistent client-side preference and localized toggle labels.
- Playwright smoke coverage for theme persistence across reload and secondary page.
- Register password mismatch UX polish (inline validation before submit, without clearing password fields).
- Self-hosted operations guide with healthchecks, manual backup/restore, and reverse-proxy example stacks.

### Considering

- Managed hosting option.
- Optional end-to-end encrypted sync (client-side key, self-hosted or managed).

## License

Ovumcy is licensed under AGPL v3.
See [LICENSE](LICENSE).
