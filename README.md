[![CI](https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml/badge.svg)](https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml)
[![CodeQL](https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml/badge.svg)](https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/ovumcy/ovumcy-web/badge)](https://securityscorecards.dev/viewer/?uri=github.com/ovumcy/ovumcy-web)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13130/badge)](https://www.bestpractices.dev/projects/13130)
[![Coverage](https://codecov.io/gh/ovumcy/ovumcy-web/graph/badge.svg)](https://app.codecov.io/gh/ovumcy/ovumcy-web)
[![Tested](https://img.shields.io/badge/tested-mutation%20%C2%B7%20fuzz%20%C2%B7%20property-2ea44f)](https://github.com/ovumcy/ovumcy-web/blob/main/TESTING.md)
[![Release](https://img.shields.io/github/v/release/ovumcy/ovumcy-web?display_name=tag)](https://github.com/ovumcy/ovumcy-web/releases)
[![Last Commit](https://img.shields.io/github/last-commit/ovumcy/ovumcy-web)](https://github.com/ovumcy/ovumcy-web/commits/main)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Reference](https://pkg.go.dev/badge/github.com/ovumcy/ovumcy-web.svg)](https://pkg.go.dev/github.com/ovumcy/ovumcy-web)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker)](https://github.com/ovumcy/ovumcy-web/actions/workflows/docker-image.yml)
[![Self-hosted](https://img.shields.io/badge/Self--hosted-yes-2ea44f)](https://github.com/ovumcy/ovumcy-web/blob/main/docs/self-hosted.md)
[![No telemetry](https://img.shields.io/badge/Telemetry-none-2ea44f)](https://github.com/ovumcy/ovumcy-web#privacy-and-security)

<p align="center">
  <img src="docs/screenshots/ovumcy-logo-horizontal.svg" alt="Ovumcy" width="640">
</p>

<p align="center">
  <strong>A menstrual cycle tracker you run yourself. Your data stays on your server.</strong>
</p>

Ovumcy is a menstrual cycle tracker you run on your own server. If the thought of your period dates, symptoms, and fertile-window estimates sitting on someone else's cloud makes you uneasy, this is for you: quick daily logging, cycle insights that are actually useful, and health data that stays on a machine you control.

Ovumcy runs as a single Go service with a server-rendered web UI, can be installed on a phone home screen, and supports SQLite by default with Postgres as an advanced self-hosted path.

This README describes the current `main` branch. The latest tagged release is `v1.7.0`.
The public project site is [ovumcy.com](https://ovumcy.com).

## Why Ovumcy Exists

Most cycle tracking apps start by asking you to sign up for a cloud account, then lean on analytics and third-party services you never really see.

Ovumcy goes the other way. You host it yourself, so the sensitive parts — your cycle history, your symptoms — stay with you. In return you get simple daily tracking and cycle insights that genuinely help, without handing your health data to anyone.

## How Ovumcy Is Different

Different trackers optimize for different things. The table below compares broad product models rather than specific brands, since privacy, export, and telemetry policies shift too often between apps to pin to names.

| Capability | Ovumcy | Local-first app | Cloud-first tracker |
| --- | --- | --- | --- |
| Self-hosted by the user or operator | :white_check_mark: | Device-local | :x: |
| No vendor account required | :white_check_mark: | :white_check_mark: | :x: |
| Multi-device browser access | :white_check_mark: | :x: | :white_check_mark: |
| No telemetry or ad trackers by product default | :white_check_mark: | Varies | Varies |
| Open data export | :white_check_mark: | Varies | Varies |
| Operator-controlled storage | :white_check_mark: | Device-only | :x: |

Ovumcy trades single-device simplicity for self-hosted control, operator-managed storage, and browser access from any device.

## Demo

<p align="center">
  <img src="docs/demo.gif" alt="Ovumcy demo — sign up, onboarding, dashboard, calendar, settings, and dark theme" width="720">
</p>

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

## Short FAQ

### Does Ovumcy require a cloud account?

No — you run it yourself, on your own server, with no vendor account in the middle.

### Where is the data stored?

On the server you deploy Ovumcy to, and nowhere else. SQLite is the default and works out of the box; PostgreSQL is there when you want it for a more involved setup.

### Does Ovumcy use analytics or ad trackers?

No. No analytics, no ad trackers, no telemetry baked in.

### Can I export my data?

Yes, and easily. Export to CSV or JSON whenever you like, so your records are always yours to take elsewhere. See [docs/export.md](docs/export.md) for the exact JSON shape, CSV columns, and stability contract.

### Is there an HTTP API specification?

Yes. The canonical JSON surface lives at `/api/v1/*` and is described in [docs/openapi.yaml](docs/openapi.yaml) (OpenAPI 3.1). `/api/v1/*` is the stable contract for external clients and wrappers — see [CONTRIBUTING.md](CONTRIBUTING.md) for the API Stability Contract. Building a wrapper? Start with `GET /api/v1/users/current` to confirm the session subject, then branch on the documented status code plus `error_detail.category` for error handling.

### Do I need technical knowledge to install Ovumcy?

Not much. If you're comfortable with the basics of Docker, the quick start will get you there — the repository ships a `docker-compose.yml` with working defaults, so you're not starting from a blank page.

### Is Ovumcy a medical product?

No. Ovumcy provides estimates and logs based on recorded data. It is not a medical device and should not be treated as diagnostic or treatment advice.

Period and fertile-window predictions in particular are statistical estimates derived from the cycle data you log. They are not a contraceptive method, a fertility treatment, or a substitute for medical care. Use a medically appropriate method when you need one.

## Features

- Log the day-to-day: period days, flow intensity, symptoms, and free-form notes.
- Your own custom symptoms — create, rename, hide, or restore them, and past entries stay intact through all of it.
- Predictions for your next period, ovulation, fertile window, and cycle phase.
- Calendar and statistics views for spotting patterns over the longer term.
- Reminders, three ways: an in-app dashboard banner, webhook reminders to your own self-hosted ntfy/Gotify endpoint, and a private, read-only calendar (`.ics`) subscription.
- Install it to your phone's home screen (on the current `main` branch).
- CSV and JSON export — for backups, for moving your data, or just for looking back.
- Optional OIDC sign-in in hybrid or SSO-only mode, with guarded owner auto-provision and provider logout.
- Optional TOTP two-factor authentication for owner sign-in, working with any RFC 6238 authenticator app (Google Authenticator, 1Password, Aegis, and the like).
- Speaks English, Russian, Spanish, French, German, and Italian.
- Runs self-hosted, either via Docker or as a single Go binary.

## How Predictions Work

Ovumcy works out ovulation, the fertile window, and your next period from the
dates you log. There are no sensors and no hormone readings involved; it is
calendar math, and the model is deliberately simple:

- Your next period is your last period start plus your typical cycle length (the
  median of your recent cycles).
- Ovulation is counted back from there. The luteal phase, from ovulation to the
  next period, is treated as about 14 days by default — and refined toward your
  own value when your temperature or cervical-mucus entries allow — so ovulation
  lands near cycle length minus the luteal length.
- The fertile window is the six days ending on ovulation day, since sperm can
  survive a few days and the egg about one.

These are estimates, not medical advice and not a form of contraception, and they
get less reliable for irregular cycles.

The full algorithm, with every constant and edge case, is in
[docs/cycle-prediction.md](docs/cycle-prediction.md). The worked examples there are
checked by reference tests, so the doc and the code stay in sync. You can read how
a prediction is made and check it against the numbers yourself.

## Reminders & Notifications

Ovumcy can surface an upcoming period or ovulation estimate through three self-hosted channels, all driven by the same prediction and the same owner-configurable lead time:

- **In-app dashboard banner** — shown automatically; the owner sets how many days ahead it appears (0–14, default 3) from Settings.
- **Webhook reminders** — the owner points Settings at their own webhook endpoint (a self-hosted ntfy, Gotify, or similar instance); delivery runs via the `ovumcy notify` CLI on your own schedule (cron, systemd timer, Docker one-shot, Task Scheduler) or an optional built-in daily scheduler, off by default (`REMINDER_SCHEDULER_ENABLED`).
- **Calendar (.ics) subscription** — the owner generates a private, read-only subscribe URL from Settings, shown once, for any calendar app that supports "subscribe by URL."

Every reminder — banner, webhook payload, and calendar feed alike — carries the same medical-safety framing as the rest of the app: these are estimates, not medical advice or a method of contraception.

Full setup steps, exact environment variables, and the CLI reference are in [docs/notifications.md](docs/notifications.md).

## Supported Languages

| Language | Code | UI support | `DEFAULT_LANGUAGE` |
| --- | --- | --- | --- |
| English | `en` | Full first-party UI localization | Supported |
| Russian | `ru` | Full first-party UI localization | Supported |
| Spanish | `es` | Full first-party UI localization | Supported |
| French | `fr` | Full first-party UI localization | Supported |
| German | `de` | Full first-party UI localization | Supported |
| Italian | `it` | Full first-party UI localization | Supported |

These are the currently supported first-party UI languages. Operators can set `DEFAULT_LANGUAGE` to any of the codes above, and users can switch language from the UI without changing deployment defaults.

## Privacy and Security

- No analytics, ad trackers, or remote telemetry.
- No telemetry and no outbound network calls in the default configuration. Outbound traffic only happens when an owner opts into it: the server talks to the configured identity provider when OIDC is enabled, and to the owner's own webhook endpoint when webhook reminders are configured. Nothing is sent to Ovumcy or any third party.
- First-party cookies only; see [SECURITY.md](SECURITY.md#cookies) for the full inventory and attributes.
- Data stays on infrastructure you control.
- Automated security checks cover CodeQL, gosec, Trivy filesystem/container scans, and CycloneDX SBOM generation in GitHub Actions.
- SQLite is the baseline default; Postgres is available for advanced self-hosted deployments through official example stacks.
- Optional TOTP 2FA: secrets are AES-256-GCM encrypted at rest with per-row aad binding, the login challenge and disable-confirmation endpoints are rate-limited, and each account carries a persistent monotonic replay floor (`totp_last_used_step`): a code at or below the last successfully consumed RFC 6238 step is rejected permanently, and the floor survives restarts.

Operator-facing GDPR compliance walkthrough lives in [docs/gdpr.md](docs/gdpr.md) (lawful basis, encryption-at-rest guidance, DSAR via export, breach notification runbook). The repo-visible security invariants live in [docs/SECURITY_INVARIANTS.md](docs/SECURITY_INVARIANTS.md); the GDPR cross-reference table is in [SECURITY.md → GDPR Cross-Reference](SECURITY.md#gdpr-cross-reference).

If you found a security issue, see [SECURITY.md](SECURITY.md).

## Clients And Deployment Models

Ovumcy now has two public product shapes:

- [`ovumcy-web`](https://github.com/ovumcy/ovumcy-web) is the self-hosted all-in-one web application and server in this repository.
- [`ovumcy-app`](https://github.com/ovumcy/ovumcy-app) is the local-first mobile client for iOS and Android.

For the mobile client, optional self-hosted encrypted sync is provided by [`ovumcy-sync-community`](https://github.com/ovumcy/ovumcy-sync-community).

In other words:

- choose `ovumcy-web` when you want one self-hosted server with a browser UI;
- choose `ovumcy-app` when you want an on-device local-first mobile experience;
- add `ovumcy-sync-community` only when the mobile app needs self-hosted encrypted backup, restore, or multi-device sync.

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

For the internal layering, trust boundaries, and request lifecycle, see [`docs/architecture.md`](docs/architecture.md).

## Tech Stack

- Backend: Go, Fiber, GORM.
- Frontend: server-rendered HTML templates, HTMX, plain JavaScript, Tailwind CSS.
- Storage: SQLite (baseline) or Postgres (advanced self-hosted).
- Deployment: Docker or direct binary execution.

## Quick Start

### Docker

Uses the prebuilt image from GHCR pinned to the latest tagged release by default (`ghcr.io/ovumcy/ovumcy-web:v1.7.0`).

Tagged releases from `v0.7.1` onward publish under the GHCR namespace `ghcr.io/ovumcy/ovumcy-web`.

**Verify the image before running (recommended).** Every published image is Cosign-signed (keyless, via GitHub Actions OIDC — no long-lived signing key), carries a SLSA build-provenance attestation, and ships an SBOM attached at build time. To verify a tagged release (needs [`cosign`](https://docs.sigstore.dev/cosign/installation/) and the [`gh`](https://cli.github.com/) CLI):

```bash
# 1. Cosign signature — pins the signer identity (this workflow) and the OIDC issuer
cosign verify \
  --certificate-identity-regexp '^https://github.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/ovumcy/ovumcy-web:v1.7.0

# 2. SLSA build provenance (GitHub attestation)
gh attestation verify oci://ghcr.io/ovumcy/ovumcy-web:v1.7.0 --repo ovumcy/ovumcy-web

# 3. SBOM attached at build time
docker buildx imagetools inspect ghcr.io/ovumcy/ovumcy-web:v1.7.0 --format '{{ json .SBOM }}'
```

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
OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web:v1.7.0 docker compose up -d
```

Then open `http://127.0.0.1:8080`.

The base compose file now binds to loopback by default. For an intentional LAN/private-network bind, set `HOST_BIND_ADDRESS` in `.env` to a specific private IP you control before starting the stack.

For production-style setups:

- use the dedicated reverse-proxy examples from [docs/self-hosted.md](docs/self-hosted.md) instead of exposing `8080` directly;
- use [docs/examples/postgres/docker-compose.yml](docs/examples/postgres/docker-compose.yml) together with [docs/examples/postgres/.env.example](docs/examples/postgres/.env.example) for the official local/private Postgres path;
- choose one storage engine per deployment, because there is no automatic SQLite-to-Postgres migration tool yet.

### Manual

Requirements:

- Go 1.26+
- Node.js 22+

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

# Per-action audit logs to stderr. Default off. Enable only when investigating an incident.
AUDIT_LOG_ENABLED=false

# Rate limits (defaults shown); see SECURITY.md's Rate Limits section for the full policy
# RATE_LIMIT_LOGIN_MAX=8
# RATE_LIMIT_LOGIN_WINDOW=15m
# RATE_LIMIT_REGISTER_MAX=8
# RATE_LIMIT_REGISTER_WINDOW=15m
# RATE_LIMIT_FORGOT_PASSWORD_MAX=8
# RATE_LIMIT_FORGOT_PASSWORD_WINDOW=1h
# RATE_LIMIT_LOGOUT_MAX=60
# RATE_LIMIT_LOGOUT_WINDOW=15m
# RATE_LIMIT_API_MAX=300
# RATE_LIMIT_API_WINDOW=1m

# Optional OIDC sign-in / SSO
# OIDC_ENABLED=true
# OIDC_ISSUER_URL=https://id.example.com
# OIDC_CLIENT_ID=ovumcy
# OIDC_CLIENT_SECRET=replace_with_a_client_secret
# OIDC_REDIRECT_URL=https://ovumcy.example.com/auth/oidc/callback
# OIDC_CA_FILE=/run/certs/oidc-provider-ca.pem
# OIDC_LOGIN_MODE=hybrid
# OIDC_RESPONSE_MODE=form_post
# OIDC_AUTO_PROVISION=false
# OIDC_AUTO_PROVISION_ALLOWED_DOMAINS=
# OIDC_LOGOUT_MODE=local
# OIDC_POST_LOGOUT_REDIRECT_URL=https://ovumcy.example.com/login
```

Important notes:

- Always set a strong secret through `SECRET_KEY` or `SECRET_KEY_FILE`.
- `SECRET_KEY_FILE` must point to a readable file path for the running process. In Docker-based deployments, that means a path inside the container after you mount the file.
- `SECRET_KEY` takes precedence if both `SECRET_KEY` and `SECRET_KEY_FILE` are set.
- `DEFAULT_LANGUAGE` supports `en`, `ru`, `es`, `fr`, `de`, and `it`.
- `REGISTRATION_MODE` supports `open` and `closed`; use `closed` for pre-provisioned or otherwise operator-restricted internet-facing instances where self-service sign-up must stay disabled.
- `HOST_BIND_ADDRESS=127.0.0.1` keeps the base compose path local/private by default. Only change it deliberately for a specific private-network bind.
- Set `COOKIE_SECURE=true` when serving over HTTPS.
- `AUDIT_LOG_ENABLED` is off by default. Per-action security-event lines are suppressed; Go panics, startup errors, and the Fiber request log stay enabled. Flip to `true` only when investigating a specific incident, and remember the resulting stream contains `user_id` and is as sensitive as the database. See [SECURITY.md](SECURITY.md#logging-policy).
- OIDC sign-in is optional, supports `hybrid` and `oidc_only` login modes, and requires HTTPS plus `COOKIE_SECURE=true`.
- `OIDC_CA_FILE` is optional and lets Ovumcy trust a readable PEM CA bundle for private or internal identity-provider certificates.
- The first OIDC sign-in uses an existing `(issuer, subject)` link when present, otherwise it falls back to a verified email match.
- `OIDC_AUTO_PROVISION=true` is supported only with `REGISTRATION_MODE=open`; it creates `owner` accounts and can be restricted with `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS`.
- Auto-provisioned users start without a local password. They can set one later in `Settings` to enable recovery codes and password-confirmed danger-zone actions.
- `OIDC_LOGOUT_MODE` controls whether logout stays local or redirects to the provider when discovery metadata includes `end_session_endpoint`.
- [docs/oidc.md](docs/oidc.md) includes a provider compatibility matrix: local-test-stack-verified support for Keycloak, authentik, and Authelia; Pocket ID 2.7.0+ reported-supported pending Ovumcy re-verification; query-only providers (Dex, better-auth, older Pocket ID) supported via `OIDC_RESPONSE_MODE=query`; and ZITADEL deployment requirements for browser sign-in. The matrix carries a "Last verified in" Ovumcy release column — see the doc for the test-stack scope and re-verification responsibility.
- Enable `TRUST_PROXY_ENABLED` only when running behind a trusted reverse proxy.
- SQLite is the supported baseline default; Postgres is an advanced self-hosted path that requires `DATABASE_URL`.
- Keep database storage persistent, whether that is a SQLite volume/bind mount or operator-managed Postgres storage.
- Full deployment, backup, reverse-proxy, and Postgres guidance lives in [docs/self-hosted.md](docs/self-hosted.md).

For deployment paths, reverse-proxy examples, backups, restores, and advanced Postgres setups, see [docs/self-hosted.md](docs/self-hosted.md). For provider-specific OIDC/SSO setup, see [docs/oidc.md](docs/oidc.md).

## Operator CLI

For self-hosted operators, the binary includes a small local-only CLI for account provisioning, audit, removal, and emergency password reset:

```bash
go run ./cmd/ovumcy users create owner@example.com
printf '%s' "$OWNER_PASSWORD" | go run ./cmd/ovumcy users create owner@example.com
go run ./cmd/ovumcy users list
go run ./cmd/ovumcy users delete owner@example.com
go run ./cmd/ovumcy users delete owner@example.com --yes
go run ./cmd/ovumcy reset-password owner@example.com
```

Notes:

- `users create <email>` provisions an owner account so an instance can be set up declaratively (for example a YunoHost install script), instead of opening registration, signing up, then closing it again. An instance can host several independent owners (household self-hosting) — run it once per person; each owner's data stays isolated by `user_id`, and only a duplicate email is rejected — pass `--skip-if-exists` to make re-runs idempotent (an existing email is skipped with a success exit code, never overwritten; use `reset-password` to change a password). On an interactive terminal it prompts for the password twice with echo disabled; when stdin is piped or redirected it reads the password from the first line of stdin, so the secret never appears in the command line or the environment. Each owner completes onboarding (last period start, cycle defaults) on first sign-in — that health data is intentionally never passed through provisioning. No recovery code is printed by default (so it cannot leak into install logs); pass `--show-recovery-code` to print it for an interactive operator, or sign in and regenerate one from Settings.
- `users list` prints a minimal account audit table: `id`, `email`, `role`, `display name`, onboarding state, and creation time.
- `users delete <email>` removes the selected account together with related health data and prompts for an explicit `DELETE` confirmation unless `--yes` is provided.
- `reset-password <email>` prompts for a new password interactively, validates it against the password policy, writes its bcrypt hash to the account, and atomically bumps `auth_session_version` so every existing session is invalidated. Use this when an owner has lost both their password and their recovery code.
- `notify` runs one webhook reminder pass — decides due period/ovulation reminders per owner and delivers them to each owner's configured webhook. It is meant to be scheduled (cron, systemd timer, a Docker one-shot, or Task Scheduler), not run continuously; an optional built-in daily scheduler (`REMINDER_SCHEDULER_ENABLED`) can run the same pass in-process instead. See [docs/notifications.md](docs/notifications.md) for all three reminder channels (in-app banner, webhook, calendar feed), scheduling recipes, and the idempotency/security contract.
- `webhook show|set <email>` inspects or configures an owner's webhook notification settings (endpoint, enabled state, notify-period/notify-ovulation toggles) from the shell — the same settings the Settings-page form writes. See [docs/notifications.md](docs/notifications.md) for the full flag reference.
- Treat CLI usage as operator-only access. It is intended for local shell access on the instance, not for browser or remote public administration.

## Development

Common commands from the repository root:

```bash
# scoped past node_modules/, where a vendored JS dep ships a .go file
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/...
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
Dedicated security workflows run CodeQL plus `gosec`, `govulncheck`, Trivy filesystem/container scanning, and publish a CycloneDX image SBOM artifact for each scan run.

Beyond plain unit and integration tests, the suite uses property-based tests,
native fuzzing, reference-vector tests for the cycle math, and mutation testing to
verify the tests themselves catch real bugs. See **[TESTING.md](TESTING.md)** for
the full quality and security approach.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

For bugs and feature requests, open a GitHub issue:
- https://github.com/ovumcy/ovumcy-web/issues

## Releases

- Latest tagged release: `v1.7.0`.
- Publish release notes via GitHub Releases and keep [CHANGELOG.md](CHANGELOG.md) updated.

## Roadmap

Product planning lives in GitHub Issues and the `Ovumcy Roadmap` project board.
This README intentionally focuses on functionality that exists today rather than future or commercial roadmap items.

## License

Ovumcy is licensed under AGPL v3.
See [LICENSE](LICENSE).

Third-party software redistributed with the built application (e.g. htmx) is listed with its
license in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
