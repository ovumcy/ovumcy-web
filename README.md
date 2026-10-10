<p align="center">
  <img src="docs/screenshots/ovumcy-logo-horizontal.svg" alt="Ovumcy" width="640">
</p>

<p align="center">
  <strong>A menstrual cycle tracker you run yourself. Your data stays on your server.</strong>
</p>

<p align="center">
  <!-- The CI badge reads the MERGE-QUEUE run, not the push that follows it: `main` is
       written only through the queue, so that run is the last attempt to enter the branch
       and it tests the exact commit that lands. The unfiltered badge read the push run,
       where any cancellation renders as failing — a job that hits `timeout-minutes` is
       reported `cancelled`, which took the badge red on 2026-08-17 with no failed job in
       the run. Do not add `branch=main`: queue runs live on `gh-readonly-queue/...`, and
       that pair returns "no status". -->
  <a href="https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml?query=event%3Amerge_group"><img src="https://github.com/ovumcy/ovumcy-web/actions/workflows/ci.yml/badge.svg?event=merge_group" alt="CI"></a>
  <a href="https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml"><img src="https://github.com/ovumcy/ovumcy-web/actions/workflows/codeql.yml/badge.svg" alt="CodeQL"></a>
  <a href="https://securityscorecards.dev/viewer/?uri=github.com/ovumcy/ovumcy-web"><img src="https://api.securityscorecards.dev/projects/github.com/ovumcy/ovumcy-web/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://www.bestpractices.dev/projects/13130"><img src="https://www.bestpractices.dev/projects/13130/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://app.codecov.io/gh/ovumcy/ovumcy-web"><img src="https://codecov.io/gh/ovumcy/ovumcy-web/graph/badge.svg" alt="Coverage"></a>
</p>

<p align="center">
  <a href="https://github.com/ovumcy/ovumcy-web/releases"><img src="https://img.shields.io/github/v/release/ovumcy/ovumcy-web?display_name=tag" alt="Release"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27.2+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="https://www.gnu.org/licenses/agpl-3.0"><img src="https://img.shields.io/badge/License-AGPL%20v3-blue.svg" alt="License: AGPL v3"></a>
  <a href="https://github.com/ovumcy/ovumcy-web/pkgs/container/ovumcy-web"><img src="https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fovumcy%2Fovumcy-web%2Fbadges%2Fpulls.json&logo=docker" alt="GHCR pulls"></a>
  <a href="https://hub.docker.com/r/ovumcy/ovumcy-web"><img src="https://img.shields.io/docker/pulls/ovumcy/ovumcy-web" alt="Docker Hub pulls"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#features">Features</a> ·
  <a href="#demo-and-screenshots">Screenshots</a> ·
  <a href="docs/self-hosted.md">Self-hosting guide</a> ·
  <a href="https://ovumcy.com">ovumcy.com</a>
</p>

Ovumcy is a menstrual cycle tracker you run on your own server. If the thought of your period dates, symptoms, and fertile-window estimates sitting on someone else's cloud makes you uneasy, this is for you: quick daily logging, cycle insights that are actually useful, and health data that stays on a machine you control.

- **One service.** A single Go binary or container with a server-rendered web UI, installable on a phone home screen.
- **Your storage.** SQLite out of the box; PostgreSQL when you want it.
- **No strings.** No vendor account, no analytics, no telemetry. CSV and JSON export whenever you like.

This README describes the current `main` branch. The latest tagged release is `v1.9.2`.

## Contents

- [Quick Start](#quick-start) · [Configuration](#configuration)
- [Demo and Screenshots](#demo-and-screenshots)
- [Why Ovumcy](#why-ovumcy) · [Features](#features) · [How Predictions Work](#how-predictions-work)
- [Reminders & Notifications](#reminders--notifications) · [Supported Languages](#supported-languages)
- [Privacy and Security](#privacy-and-security) · [Short FAQ](#short-faq)
- [Architecture](#architecture) · [Operator CLI](#operator-cli) · [Related Projects](#related-projects)
- [Development](#development) · [Contributing](#contributing) · [Releases](#releases) · [License](#license)

## Quick Start

### Docker

```bash
mkdir -p ovumcy && cd ovumcy
curl -fsSL -o docker-compose.yml https://raw.githubusercontent.com/ovumcy/ovumcy-web/main/docker-compose.yml
curl -fsSL -o .env https://raw.githubusercontent.com/ovumcy/ovumcy-web/main/.env.example
# set SECRET_KEY in .env, or mount a secret file and set SECRET_KEY_FILE
docker compose up -d
```

Then open `http://127.0.0.1:8080`.

The compose file pulls the latest tagged release, `ghcr.io/ovumcy/ovumcy-web:v1.9.2`, from GHCR; no GitHub login is needed, and `pull_policy: always` keeps the tag fresh on every restart. The same image is mirrored to Docker Hub as `docker.io/ovumcy/ovumcy-web` under the same tags and at the same digest. To run a different tag, set `OVUMCY_IMAGE` in `.env` or on the command line:

```bash
OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web:v1.9.2 docker compose up -d
```

The base compose file binds to loopback. For a deliberate LAN or private-network bind, set `HOST_BIND_ADDRESS` in `.env` to a specific private IP you control; only compose reads it, the binary itself listens on `PORT` on every interface.

For production-style setups:

- put Ovumcy behind a reverse proxy with the examples in [docs/self-hosted.md](docs/self-hosted.md) instead of exposing `8080` directly;
- for Postgres, start from [docs/examples/postgres/docker-compose.yml](docs/examples/postgres/docker-compose.yml) and [docs/examples/postgres/.env.example](docs/examples/postgres/.env.example);
- choose one storage engine per deployment: there is no SQLite-to-Postgres migration tool yet.

> **Upgrading an existing install to v2.0.0 or later? Take the new compose file first.** The new image expects the `ovumcy_fence` volume at `/app/fence`, and the new compose files forward `.env` keys an older copy silently ignored (`REGISTRATION_MODE` among them), so values already sitting in `.env` take effect at the first start. Follow the [Safe Upgrade Procedure](docs/self-hosted.md#safe-upgrade-procedure).

<details>
<summary><strong>Verify the image before running it (recommended)</strong></summary>

Every published image is Cosign-signed (keyless, via GitHub Actions OIDC — no long-lived signing key), carries a SLSA build-provenance attestation, and ships an SBOM attached at build time. Verifying a tagged release needs [`cosign`](https://docs.sigstore.dev/cosign/installation/) and the [`gh`](https://cli.github.com/) CLI:

```bash
# 1. Cosign signature — pins the signer identity (this workflow) and the OIDC issuer
cosign verify \
  --certificate-identity-regexp '^https://github\.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/ovumcy/ovumcy-web:v1.9.2

# 2. SLSA build provenance (GitHub attestation)
gh attestation verify oci://ghcr.io/ovumcy/ovumcy-web:v1.9.2 --repo ovumcy/ovumcy-web

# 3. SBOM attached at build time
docker buildx imagetools inspect ghcr.io/ovumcy/ovumcy-web:v1.9.2 --format '{{ json .SBOM }}'
```

The Docker Hub mirror answers to the same checks: substitute `docker.io/ovumcy/ovumcy-web` for the GHCR name. The mirror is a copy of the signed manifest, not a second build; it is signed on Docker Hub itself by the same workflow identity over the same digest, and the release workflow refuses to report the mirror published until it has run the signature check above against Docker Hub. Details: [SECURITY.md](SECURITY.md#verifying-release-authenticity).

The `v1.9.2` tag is mutable and `pull_policy: always` re-pulls it on every restart, so a one-time check does not by itself guarantee later restarts run the same bytes. To pin the exact image you verified, set `OVUMCY_IMAGE` to its digest instead of the tag:

```bash
# Resolve the digest of the tag you just verified, then pin it in .env:
docker buildx imagetools inspect ghcr.io/ovumcy/ovumcy-web:v1.9.2 --format '{{ .Manifest.Digest }}'
# .env → OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web@sha256:<digest>
```

**An untagged, unsigned digest can exist in the GHCR package, and it is safe to ignore.** The release workflow pushes the image by digest, scans every platform, and only then signs and tags it. When the scan refuses, or a step between the push and the signature fails, that digest stays in the public package: no tag, no signature, still pullable by digest, and printed in the failed run's log. Nothing you are told to run resolves to it — every command above names a tag, every tag the workflow writes points at a signed digest, and the Cosign check in step 1 fails on a digest that was never signed. It is kept rather than deleted on purpose: deleting it would need delete rights over the whole package inside the publish job — a `delete:packages` token, or the Admin role for this repository's `GITHUB_TOKEN` on the package — and either would let a compromised publish step delete signed releases too, a larger risk than an unsigned digest that verification already refuses.

</details>

### From source

A binary started this way listens on `PORT` (default `8080`) on every interface; `HOST_BIND_ADDRESS` has no effect outside compose. On a machine other hosts can reach, block the port with a firewall.

Requirements:

- Go 1.27.2+
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

## Demo and Screenshots

<p align="center">
  <img src="docs/demo.gif" alt="Ovumcy demo — sign up, onboarding, dashboard, calendar, settings, and dark theme" width="720">
</p>

<table>
  <tr>
    <th>Check today at a glance</th>
    <th>Review the month</th>
  </tr>
  <tr>
    <td><img src="docs/screenshots/dashboard.jpg" alt="Ovumcy dashboard screen"></td>
    <td><img src="docs/screenshots/calendar.jpg" alt="Ovumcy calendar screen"></td>
  </tr>
  <tr>
    <th>Export what you need</th>
    <th>Use a comfortable dark theme</th>
  </tr>
  <tr>
    <td><img src="docs/screenshots/settings-export.jpg" alt="Ovumcy export settings screen"></td>
    <td><img src="docs/screenshots/dark-theme.jpg" alt="Ovumcy dark theme screen"></td>
  </tr>
  <tr>
    <th>Get started quickly</th>
    <th>Install it on a phone</th>
  </tr>
  <tr>
    <td><img src="docs/screenshots/register.jpg" alt="Ovumcy registration screen"></td>
    <td><img src="docs/screenshots/install-prompt.png" alt="Ovumcy mobile install prompt"></td>
  </tr>
</table>

The privacy-safe hero demo asset pack, including the mobile install prompt capture contract, lives in [docs/hero-demo.md](docs/hero-demo.md).

## Why Ovumcy

Most cycle tracking apps start by asking you to sign up for a cloud account, then lean on analytics and third-party services you never really see.

Ovumcy goes the other way. You host it yourself, so the sensitive parts — your cycle history, your symptoms — stay with you. In return you get simple daily tracking and cycle insights that genuinely help, without handing your health data to anyone.

The table compares broad product models rather than specific brands, since privacy, export, and telemetry policies shift too often between apps to pin to names.

| Capability | Ovumcy | Local-first app | Cloud-first tracker |
| --- | --- | --- | --- |
| Self-hosted by the user or operator | :white_check_mark: | Device-local | :x: |
| No vendor account required | :white_check_mark: | :white_check_mark: | :x: |
| Multi-device browser access | :white_check_mark: | :x: | :white_check_mark: |
| No telemetry or ad trackers by product default | :white_check_mark: | Varies | Varies |
| Open data export | :white_check_mark: | Varies | Varies |
| Operator-controlled storage | :white_check_mark: | Device-only | :x: |

Ovumcy trades single-device simplicity for self-hosted control, operator-managed storage, and browser access from any device.

## Features

- Log the day-to-day: period days, flow intensity, symptoms, and free-form notes.
- Your own custom symptoms — create, rename, hide, or restore them, and past entries stay intact through all of it.
- Predictions for your next period, ovulation, fertile window, and cycle phase.
- Calendar and statistics views for spotting patterns over the longer term.
- Reminders, three ways: an in-app dashboard banner, webhook reminders to your own self-hosted ntfy/Gotify endpoint, and a private, read-only calendar (`.ics`) subscription.
- Install it to your phone's home screen — a web app manifest and install prompt, no service worker or offline cache.
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

| Language | Code |
| --- | --- |
| English | `en` |
| Russian | `ru` |
| Spanish | `es` |
| French | `fr` |
| German | `de` |
| Italian | `it` |

All six are full first-party UI localizations. Operators set the instance default with `DEFAULT_LANGUAGE` (any code above); users switch language from the UI without changing deployment defaults.

## Privacy and Security

- No analytics, ad trackers, or remote telemetry, and no outbound network calls in the default configuration. Outbound traffic only happens when an owner opts into it: the server talks to the configured identity provider when OIDC is enabled, and to the owner's own webhook endpoint when webhook reminders are configured. Nothing is ever sent to the Ovumcy project, and no egress happens that the owner did not configure; see [docs/security/data-handling.md](docs/security/data-handling.md) for the canonical egress statement.
- First-party cookies only; see [docs/security/cryptography.md](docs/security/cryptography.md#cookies) for the full inventory and attributes.
- Data stays on infrastructure you control: SQLite by default, Postgres through the official example stacks.
- Automated security checks cover CodeQL, gosec, Trivy filesystem/container scans, and CycloneDX SBOM generation in GitHub Actions.
- Every published image is signed and attested; see [Verify the image](#quick-start) above.

The security documentation index, the threat model, and the test-enforcement matrix are in [SECURITY.md](SECURITY.md). If you found a security issue, see its [Reporting a Vulnerability](SECURITY.md#reporting-a-vulnerability) section.

## Short FAQ

### Does Ovumcy require a cloud account?

No — you run it yourself, on your own server, with no vendor account in the middle.

### Where is the data stored?

On the server you deploy Ovumcy to. SQLite is the default and works out of the box; PostgreSQL is there when you want it for a more involved setup. Nothing leaves that server unless you switch it on yourself: the optional webhook reminders POST the predicted dates to a URL you choose, and a calendar app you subscribe to the `.ics` feed fetches those dates wherever it runs — onto Google's or Apple's servers if that is where your calendar lives.

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

The essentials:

- Always set a strong secret through `SECRET_KEY` or `SECRET_KEY_FILE`. `SECRET_KEY_FILE` must be a path the running process can read — inside the container, for Docker deployments — and `SECRET_KEY` wins if both are set.
- `DEFAULT_LANGUAGE` accepts `en`, `ru`, `es`, `fr`, `de`, and `it`.
- `REGISTRATION_MODE` is `open` or `closed`; use `closed` for pre-provisioned or otherwise operator-restricted internet-facing instances where self-service sign-up must stay disabled.
- Set `COOKIE_SECURE=true` when serving over HTTPS, and enable `TRUST_PROXY_ENABLED` only behind a trusted reverse proxy.
- `HOST_BIND_ADDRESS=127.0.0.1` keeps the base compose path local. It is a compose setting, not an app setting: the binary ignores it and listens on `PORT` on every interface.
- `AUDIT_LOG_ENABLED` is off by default. Flip it to `true` only while investigating a specific incident: the resulting stream contains `user_id` and is as sensitive as the database. See [docs/security/logging.md](docs/security/logging.md#logging-policy).
- Keep database storage persistent, whether that is a SQLite volume/bind mount or operator-managed Postgres storage. Postgres requires `DATABASE_URL`.

Beyond the essentials:

- **Every variable, with defaults and comments:** [.env.example](.env.example).
- **Rate limits** (`RATE_LIMIT_*`): each limiter has a ceiling it cannot be widened past, let alone switched off; an out-of-range value is logged at boot and the default is used. Policy and ranges: [SECURITY.md › Rate Limits](SECURITY.md#rate-limits).
- **OIDC / SSO** (`OIDC_*`): optional, `hybrid` or `oidc_only` login modes, requires HTTPS plus `COOKIE_SECURE=true`. A verified email claim matching an existing local account does **not** link automatically; linking needs a password-confirmed step-up or the operator CLI. Setup, the account-linking contract, and the provider compatibility matrix (Keycloak, authentik, Authelia, Pocket ID, Dex, ZITADEL): [docs/oidc.md](docs/oidc.md).
- **Deployment, reverse proxies, backups, restores, upgrades, Postgres:** [docs/self-hosted.md](docs/self-hosted.md).

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

- **Browser UI**: server-rendered HTML templates with HTMX, plain JavaScript, and Tailwind CSS, plus mobile home-screen install support.
- **Go application**: a single service on Fiber and GORM that handles routing, templates, i18n, and domain logic.
- **Storage**: SQLite is the baseline default; Postgres is an advanced self-hosted option.
- **Deployment**: one binary or container, typically behind a reverse proxy.

For the internal layering, trust boundaries, and request lifecycle, see [docs/architecture.md](docs/architecture.md).

## Operator CLI

The binary includes a small local-only CLI for account provisioning, audit, removal, and emergency password reset. Treat it as operator-only access from a shell on the instance, never as remote administration.

```bash
go run ./cmd/ovumcy users create owner@example.com
printf '%s' "$OWNER_PASSWORD" | go run ./cmd/ovumcy users create owner@example.com
go run ./cmd/ovumcy users list
go run ./cmd/ovumcy users delete owner@example.com
go run ./cmd/ovumcy users delete owner@example.com --yes
go run ./cmd/ovumcy users delete --id 7
go run ./cmd/ovumcy users set-email --id 7 owner@example.com
go run ./cmd/ovumcy reset-password owner@example.com
go run ./cmd/ovumcy reset-password --id 7
go run ./cmd/ovumcy repair symptom-names
```

- `users create <email>` provisions an owner account, so an install script can set an instance up without opening registration, signing up, then closing it again. Run it once per person for household self-hosting: each owner's data stays isolated by `user_id`. `--skip-if-exists` makes re-runs idempotent (an existing email is skipped, never overwritten). The password never touches argv or the environment: an interactive terminal prompts twice with echo disabled, piped stdin supplies it as the first line. No recovery code is printed unless you pass `--show-recovery-code`, so none leaks into install logs. No health data passes through provisioning; each owner completes onboarding on first sign-in.
- `users list` prints a minimal audit table: `id`, `email`, `role`, display name, onboarding state, and creation time.
- `users delete <email>|--id <id>` removes the account together with its health data, after an explicit `DELETE` confirmation that quotes the stored address, id, and role — unless `--yes` is given. An address matching more than one row is refused, naming the ids; retry with `--id`.
- `users set-email --id <id> <email>` re-homes one account to a new address. It is the repair for an account whose stored email predates strict sign-in normalization and can no longer be signed in to or addressed by email; it validates the new address under the sign-in rule, refuses one another account already answers to, leaves the health record untouched, and bumps `auth_session_version` because the address is the login identity. Runbook: [docs/self-hosted.md](docs/self-hosted.md#an-account-cannot-sign-in-after-upgrading-email-stored-in-a-legacy-form).
- `reset-password <email>|--id <id>` prompts for a new password, validates it against the password policy, writes its bcrypt hash, and atomically bumps `auth_session_version` so every existing session is invalidated. Use it when an owner has lost both their password and their recovery code. `--id` exists for the same legacy-row reason as `set-email`, and an ambiguous address is refused rather than resolved to whichever row the database returns first.
- `notify` runs one webhook reminder pass and is meant to be scheduled (cron, systemd timer, Docker one-shot, Task Scheduler), not run continuously; `REMINDER_SCHEDULER_ENABLED` runs the same pass in-process instead. `webhook show|set <email>` inspects or configures an owner's webhook settings from the shell, the same settings the Settings page writes; it has no `--id` form and refuses an ambiguous address. Both: [docs/notifications.md](docs/notifications.md).
- `repair` lists the offline data repairs the binary carries; each inspects and reports by default and changes nothing until `--apply`. Unlike every other subcommand it opens the database *without* applying migrations, because it exists for the case where a migration is refusing to run — which also stops the server, so there is no application to fix the data in. Today it carries `symptom-names`, for an account holding two symptoms under the same name. Runbook: [docs/self-hosted.md](docs/self-hosted.md#duplicate-rows-that-refuse-a-migration).

Running the CLI against the shell-free container, and which subcommands need the server's restore fence: [docs/self-hosted.md](docs/self-hosted.md#running-the-operator-cli-against-the-container).

## Related Projects

- [`ovumcy-web`](https://github.com/ovumcy/ovumcy-web) — this repository: the self-hosted all-in-one web application and server. Choose it for one server with a browser UI reachable from any device.
- [`ovumcy-app`](https://github.com/ovumcy/ovumcy-app) — the local-first mobile client for iOS and Android. Choose it for an on-device experience.
- [`ovumcy-sync-community`](https://github.com/ovumcy/ovumcy-sync-community) — optional self-hosted encrypted sync for the mobile app. Add it only when the app needs self-hosted backup, restore, or multi-device sync.

## Development

Common commands from the repository root:

```bash
# scoped past node_modules/, where a vendored JS dep ships a .go file;
# -timeout 30m raises Go's 10-minute PER PACKAGE default, which internal/api
# outruns on a dev host (see TESTING.md)
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... -timeout 30m
npm run build
go run ./cmd/ovumcy
```

Project structure:

- `cmd/ovumcy` - application entrypoint and runtime bootstrap
- `internal/api` - HTTP transport, handlers, and response mapping
- `internal/services` - domain logic
- `internal/db` - persistence and migrations
- `web/` - templates, JavaScript, and CSS assets

CI runs staticcheck, `go vet`, tests, and the frontend build on pull requests and on the merge queue's validation of the commit that lands.
The post-merge run on `main` deliberately skips that work — the queue already proved this exact commit — and executes only the work the queue does not: the cross-browser e2e, image-smoke and Postgres-smoke lanes, and the image publish.
Dedicated security workflows run CodeQL plus `gosec`, `govulncheck`, Trivy filesystem/container scanning, and publish a CycloneDX image SBOM artifact for each scan run.

Beyond plain unit and integration tests, the suite uses property-based tests,
native fuzzing, reference-vector tests for the cycle math, and mutation testing to
verify the tests themselves catch real bugs. See **[TESTING.md](TESTING.md)** for
the full quality and security approach.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). For bugs and feature requests, open a [GitHub issue](https://github.com/ovumcy/ovumcy-web/issues).

## Releases

- Latest tagged release: `v1.9.2`.
- Release notes are published via GitHub Releases and [CHANGELOG.md](CHANGELOG.md).
- Product planning lives in GitHub Issues and the `Ovumcy Roadmap` project board. This README describes functionality that exists today, not roadmap items.

## License

Copyright (C) 2026 Ovumcy Contributors.

Ovumcy is licensed under AGPL v3. See [LICENSE](LICENSE) for the full text of the license.

If you run a modified version of Ovumcy as a network service, section 13 of the license
obliges you to make the complete source code of that version available to its users.

Third-party software redistributed with the built application (e.g. htmx) is listed with its
license in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
