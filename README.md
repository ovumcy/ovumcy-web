[![CI](https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml/badge.svg)](https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml)
[![CodeQL](https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml/badge.svg)](https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml)
[![Coverage](https://codecov.io/gh/ovumcy/ovumcy-web/graph/badge.svg)](https://app.codecov.io/gh/ovumcy/ovumcy-web)
[![Go Report Card](https://goreportcard.com/badge/github.com/ovumcy/ovumcy-web)](https://goreportcard.com/report/github.com/ovumcy/ovumcy-web)
[![Release](https://img.shields.io/github/v/release/ovumcy/ovumcy-web?display_name=tag)](https://github.com/ovumcy/ovumcy-web/releases)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](https://github.com/ovumcy/ovumcy-web/actions/workflows/docker-image.yml)
[![Self-hosted](https://img.shields.io/badge/Self--hosted-yes-2ea44f)](https://github.com/ovumcy/ovumcy-web/blob/main/docs/self-hosted.md)
[![No telemetry](https://img.shields.io/badge/Telemetry-none-2ea44f)](https://github.com/ovumcy/ovumcy-web#privacy-and-security)

<p align="center">
  <img src="docs/screenshots/ovumcy-logo-horizontal.svg" alt="Ovumcy" width="640">
</p>

Ovumcy is a privacy-first, self-hosted menstrual cycle tracker.
It is built for people who want fast daily tracking, useful cycle insights, and data that stays under their control.

Ovumcy runs as a single Go service with a server-rendered web UI, can be installed on a phone home screen, and supports SQLite by default with Postgres as an advanced self-hosted path.

This README describes the current `main` branch. The latest tagged release is `v0.7.0`.

## Why Ovumcy Exists

Most cycle tracking apps depend on cloud accounts, analytics, or third-party infrastructure.

Ovumcy is designed as a self-hosted alternative for people who want simple daily tracking, useful cycle insights, and full control over sensitive health data.

## How Ovumcy Is Different

Different cycle trackers optimize for different things. Here is how the product models compare.

This comparison focuses on product models rather than specific brands, because privacy, export, and telemetry policies vary between apps.

| Capability | Ovumcy | Local-first app | Cloud-first tracker |
| --- | --- | --- | --- |
| Self-hosted by the user or operator | :white_check_mark: | Device-local | :x: |
| No vendor account required | :white_check_mark: | :white_check_mark: | :x: |
| Multi-device browser access | :white_check_mark: | :x: | :white_check_mark: |
| No telemetry or ad trackers by product default | :white_check_mark: | Varies | Varies |
| Open data export | :white_check_mark: | Varies | Varies |
| Operator-controlled storage | :white_check_mark: | Device-only | :x: |

Ovumcy trades single-device simplicity for self-hosted control, operator-managed storage, and browser access from any device.

## Short FAQ

### Does Ovumcy require a cloud account?

No. Ovumcy is designed to run as a self-hosted application under your control.

### Where is the data stored?

On the server where you deploy Ovumcy. SQLite is the default baseline, and PostgreSQL is available for more advanced self-hosted setups.

### Does Ovumcy use analytics or ad trackers?

No. Ovumcy is designed without telemetry or advertising trackers.

### Can I export my data?

Yes. Ovumcy supports CSV and JSON export so your records stay portable.

### Do I need technical knowledge to install Ovumcy?

Basic familiarity with Docker is enough for the supported quick start. A `docker-compose.yml` with working defaults is included in the repository.

### Is Ovumcy a medical product?

No. Ovumcy provides estimates and logs based on recorded data. It is not a medical device and should not be treated as diagnostic or treatment advice.

## Screenshots

### Get Started Quickly

![Ovumcy registration screen](docs/screenshots/register.jpg)

### Check Today at a Glance

![Ovumcy dashboard screen](docs/screenshots/dashboard.jpg)

### Review the Month

![Ovumcy calendar screen](docs/screenshots/calendar.jpg)

### Export What You Need

![Ovumcy export settings screen](docs/screenshots/settings-export.jpg)

### Install It on a Phone

![Ovumcy mobile install prompt](docs/screenshots/install-prompt.png)

### Use a Comfortable Dark Theme

![Ovumcy dark theme screen](docs/screenshots/dark-theme.jpg)

The privacy-safe hero demo asset pack, including the mobile install prompt capture contract, lives in [docs/hero-demo.md](docs/hero-demo.md).

## Features

- Daily tracking for period days, flow intensity, symptoms, and notes.
- Owner-managed custom symptoms with create, rename, hide, and restore flows that preserve historical records.
- Predictions for next period, ovulation, fertile window, and cycle phase.
- Calendar and statistics views for longer-term pattern spotting.
- Mobile home-screen install support on the current `main` branch.
- CSV, JSON, and PDF export for backup, portability, and personal review.
- English, Russian, and Spanish localization.
- Self-hosted deployment with Docker or a single Go binary.

## Supported Languages

| Language | Code | UI support | `DEFAULT_LANGUAGE` |
| --- | --- | --- | --- |
| English | `en` | Full first-party UI localization | Supported |
| Russian | `ru` | Full first-party UI localization | Supported |
| Spanish | `es` | Full first-party UI localization | Supported |
| French | `fr` | Full first-party UI localization | Supported |

These are the currently supported first-party UI languages. Operators can set `DEFAULT_LANGUAGE` to any of the codes above, and users can switch language from the UI without changing deployment defaults.

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

- `Browser UI`: server-rendered HTML with HTMX and plain JavaScript, plus mobile home-screen install support.
- `Go application`: a single service that handles routing, templates, i18n, and domain logic.
- `Storage`: SQLite is the baseline default; Postgres is an advanced self-hosted option.
- `Deployment`: one binary or container, typically behind a reverse proxy.

## Tech Stack

- Backend: Go, Fiber, GORM.
- Frontend: server-rendered HTML templates, HTMX, plain JavaScript, Tailwind CSS.
- Storage: SQLite (baseline) or Postgres (advanced self-hosted).
- Deployment: Docker or direct binary execution.

## Quick Start

### Docker

Uses the prebuilt image from GHCR pinned to the latest tagged release by default (`ghcr.io/terraincognita07/ovumcy:v0.7.0`).

The GitHub repository now lives at `github.com/ovumcy/ovumcy-web`. The current `v0.7.0` examples keep the historical GHCR image path until the next tagged release is published under the new namespace.

For public GHCR images, pull does not require GitHub login. `docker compose up -d` is enough because `pull_policy: always` is enabled.

```bash
mkdir -p ovumcy && cd ovumcy
curl -fsSL -o docker-compose.yml https://raw.githubusercontent.com/ovumcy/ovumcy-web/main/docker-compose.yml
curl -fsSL -o .env https://raw.githubusercontent.com/ovumcy/ovumcy-web/main/.env.example
# set SECRET_KEY in .env, or mount a secret file and set SECRET_KEY_FILE
docker compose up -d
```

Override the pinned default image tag if needed:

```bash
OVUMCY_IMAGE=ghcr.io/terraincognita07/ovumcy:v0.7.0 docker compose up -d
```

Then open `http://127.0.0.1:8080`.

The base compose file now binds to loopback by default. For an intentional LAN/private-network bind, set `HOST_BIND_ADDRESS` in `.env` to a specific private IP you control before starting the stack.

For production-style setups:

- use the dedicated reverse-proxy examples from [docs/self-hosted.md](docs/self-hosted.md) instead of exposing `8080` directly;
- use [docs/examples/postgres/docker-compose.yml](docs/examples/postgres/docker-compose.yml) together with [docs/examples/postgres/.env.example](docs/examples/postgres/.env.example) for the official local/private Postgres path;
- choose one storage engine per deployment, because there is no automatic SQLite-to-Postgres migration tool yet.

### Manual

Requirements:

- Go 1.24+
- Node.js 18+

```bash
git clone https://github.com/ovumcy/ovumcy-web.git
cd ovumcy-web
npm ci
npm run build
export SECRET_KEY="$(node -e "console.log(require('crypto').randomBytes(32).toString('hex'))")"
go run ./cmd/ovumcy
```

Or keep the secret in a readable file and point `SECRET_KEY_FILE` at that path:

```bash
export SECRET_KEY_FILE=/absolute/path/to/ovumcy-secret.txt
go run ./cmd/ovumcy
```

PowerShell:

```powershell
$env:SECRET_KEY = node -e "console.log(require('crypto').randomBytes(32).toString('hex'))"
go run ./cmd/ovumcy
```

```powershell
$env:SECRET_KEY_FILE = "C:\\path\\to\\ovumcy-secret.txt"
go run ./cmd/ovumcy
```

## Configuration

Most self-hosted setups only need a small set of variables:

```env
TZ=UTC
DEFAULT_LANGUAGE=en
REGISTRATION_MODE=open
# Set one secret source before first start. SECRET_KEY wins if both are set.
SECRET_KEY=
# SECRET_KEY_FILE=/run/secrets/ovumcy_secret_key
PORT=8080
HOST_BIND_ADDRESS=127.0.0.1
COOKIE_SECURE=false

DB_DRIVER=sqlite
DB_PATH=data/ovumcy.db
# DATABASE_URL=postgres://ovumcy:change-me@127.0.0.1:5432/ovumcy?sslmode=disable

TRUST_PROXY_ENABLED=false
PROXY_HEADER=X-Forwarded-For
TRUSTED_PROXIES=127.0.0.1,::1
```

Important notes:

- Always set a strong secret through `SECRET_KEY` or `SECRET_KEY_FILE`.
- `SECRET_KEY_FILE` must point to a readable file path for the running process. In Docker-based deployments, that means a path inside the container after you mount the file.
- `SECRET_KEY` takes precedence if both `SECRET_KEY` and `SECRET_KEY_FILE` are set.
- `DEFAULT_LANGUAGE` supports `en`, `ru`, `es`, and `fr`.
- `REGISTRATION_MODE` supports `open` and `closed`; use `closed` for pre-provisioned or otherwise operator-restricted internet-facing instances where self-service sign-up must stay disabled.
- `HOST_BIND_ADDRESS=127.0.0.1` keeps the base compose path local/private by default. Only change it deliberately for a specific private-network bind.
- Set `COOKIE_SECURE=true` when serving over HTTPS.
- Enable `TRUST_PROXY_ENABLED` only when running behind a trusted reverse proxy.
- SQLite is the supported baseline default; Postgres is an advanced self-hosted path that requires `DATABASE_URL`.
- Keep database storage persistent, whether that is a SQLite volume/bind mount or operator-managed Postgres storage.
- Full deployment, backup, reverse-proxy, and Postgres guidance lives in [docs/self-hosted.md](docs/self-hosted.md).

For deployment paths, reverse-proxy examples, backups, restores, and advanced Postgres setups, see [docs/self-hosted.md](docs/self-hosted.md).

## Operator CLI

For self-hosted operators, the binary includes a small local-only CLI for account audit and removal:

```bash
go run ./cmd/ovumcy users list
go run ./cmd/ovumcy users delete owner@example.com
go run ./cmd/ovumcy users delete owner@example.com --yes
```

Notes:

- `users list` prints a minimal account audit table: `id`, `email`, `role`, `display name`, onboarding state, and creation time.
- `users delete <email>` removes the selected account together with related health data and prompts for an explicit `DELETE` confirmation unless `--yes` is provided.
- Treat CLI usage as operator-only access. It is intended for local shell access on the instance, not for browser or remote public administration.

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
- https://github.com/ovumcy/ovumcy-web/issues

## Releases

- Latest tagged release: `v0.7.0`.
- Publish release notes via GitHub Releases and keep [CHANGELOG.md](CHANGELOG.md) updated.

## Roadmap

Product planning lives in GitHub Issues and the `Ovumcy Roadmap` project board.
This README intentionally focuses on functionality that exists today rather than future or commercial roadmap items.

## License

Ovumcy is licensed under AGPL v3.
See [LICENSE](LICENSE).
