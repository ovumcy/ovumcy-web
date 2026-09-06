# Self-Hosted Operations Guide

Ovumcy's supported self-hosted baseline is a single application instance with a persistent SQLite volume, HTTPS at the edge, and a strong application secret. The goal of this guide is not to describe every possible deployment, but to define a production-safe path that ordinary self-hosters can follow without inventing their own operational rules.

## Contents

**Setting up**
- [Baseline Contract](#baseline-contract) · [Production Checklist](#production-checklist)
- [Configuration Profiles](#configuration-profiles) — [required](#required-in-all-deployments), [local/private](#localprivate-base-compose-path), [public reverse-proxy](#public-reverse-proxy-stack), [advanced knobs](#advanced-knobs)
- [Optional OIDC Sign-In](#optional-oidc-sign-in) · [Privacy Responsibility Split](#privacy-responsibility-split)

**Putting it behind a proxy**
- [Reverse Proxy and HTTPS Contract](#reverse-proxy-and-https-contract) · [Reverse Proxy Examples](#reverse-proxy-examples)
- [Local/Private Postgres Stack](#official-localprivate-postgres-stack) · [Public Postgres Reverse-Proxy Stacks](#official-public-postgres-reverse-proxy-stacks)

**Running it**
- [Health Checks by Deployment Mode](#health-checks-by-deployment-mode) — [operator CLI](#running-the-operator-cli-against-the-container) · [Concurrency on the SQLite Baseline](#concurrency-on-the-sqlite-baseline) · [Secret Handling and Rotation](#secret-handling-and-rotation)
- [Backup and Restore Contract](#backup-and-restore-contract) — [volume backup](#docker-named-volume-backup), [volume restore](#docker-named-volume-restore), [post-restore verification](#post-restore-verification)
- [Calendar Feed Restore Fence](#calendar-feed-restore-fence)
- [Safe Upgrade Procedure](#safe-upgrade-procedure) · [Downgrade Caveats](#downgrade-caveats) · [Duplicate rows that refuse a migration](#duplicate-rows-that-refuse-a-migration)

**When something breaks**
- [Troubleshooting Baseline](#troubleshooting-baseline) · [Common Operator Scenarios](#common-operator-scenarios) · [Advanced Deployment Path](#advanced-deployment-path)

## Baseline Contract

Supported baseline assumptions:

- One Ovumcy instance per private deployment.
- Persistent storage for `/app/data`.
- A second, separate persistent location for `/app/fence`, which must **not** be part of your database backups. It holds the calendar-feed restore fence: [Calendar Feed Restore Fence](#calendar-feed-restore-fence).
- HTTPS termination at a trusted reverse proxy or load balancer.
- `COOKIE_SECURE=true` when traffic is served over HTTPS.
- `TRUST_PROXY_ENABLED=true` only when Ovumcy is actually behind your own trusted reverse proxy.
- Prefer a containerized reverse proxy stack where only the proxy publishes host ports.
- Keep Ovumcy's plain HTTP port internal to a private network or loopback-only.
- A strong, unique application secret provided through `SECRET_KEY` or `SECRET_KEY_FILE`.
- Optional OIDC is supported with `hybrid` and `oidc_only` login modes; it requires HTTPS, `COOKIE_SECURE=true`, and an `OIDC_REDIRECT_URL` that ends in `/auth/oidc/callback`.

Out of scope for this baseline:

- Hosted multi-tenant deployments.
- Shared databases across multiple independent users or organizations.
- Backup automation and disaster recovery orchestration beyond the manual operator workflow described here.

## Production Checklist

Before exposing Ovumcy outside localhost:

1. Generate a strong application secret, then either set `SECRET_KEY` directly or mount a readable secret file and point `SECRET_KEY_FILE` at its in-container path.
2. Use a persistent Docker volume or bind mount for the database path.
3. Put the app behind HTTPS and set `COOKIE_SECURE=true`.
4. Enable `TRUST_PROXY_ENABLED=true` only if the reverse proxy is under your control.
5. Set `TRUSTED_PROXIES` to the exact proxy IPs or network ranges you trust.
6. Prefer a reverse proxy stack where the app service has no published host port at all.
7. Restrict who can access container logs, `.env`, backups, and the SQLite data volume.
8. Verify the container becomes healthy before relying on the deployment.

## Configuration Profiles

Treat configuration in three layers instead of one flat checklist.

### Required in all deployments

- Configure at least one strong secret source: `SECRET_KEY` directly or `SECRET_KEY_FILE` pointing to a readable in-container path. `SECRET_KEY` takes precedence if both are set.
- The underlying application secret must stay private and be backed up separately from SQLite data.
- `DB_DRIVER` must match the actual runtime you intend to use.
- Persistent database storage must exist for the engine you selected.
- You must know whether you are running the local/private base compose path or a public reverse-proxy stack before changing cookie and proxy settings.

### Local/private base compose path

Use the repository root `docker-compose.yml` for localhost, LAN, or other private-network deployments:

- `HOST_BIND_ADDRESS=127.0.0.1` is the safe default and keeps the app bound to loopback on the host.
- If you intentionally want base-compose access from a private LAN, set `HOST_BIND_ADDRESS` to the specific private host IP you control before starting the stack.
- `COOKIE_SECURE=false` unless you terminate HTTPS before the app.
- `TRUST_PROXY_ENABLED=false` unless you have explicitly placed Ovumcy behind your own trusted proxy.
- `PORT=8080` is the internal app port and is also used for the host publish target in the base compose path.

### Public reverse-proxy stack

Use one of the example stacks under `docs/examples/reverse-proxy/` for public HTTPS deployments:

- `COOKIE_SECURE=true`
- `TRUST_PROXY_ENABLED=true`
- `PROXY_HEADER=X-Real-IP` — see [Reverse Proxy and HTTPS Contract](#reverse-proxy-and-https-contract) below for why this must not point at `X-Forwarded-For`.
- `TRUSTED_PROXIES` must match the exact proxy IP or private Docker subnet used by that stack
- with `COOKIE_SECURE=true`, Ovumcy emits `Strict-Transport-Security: max-age=31536000; includeSubDomains` itself (HSTS defaults to the `COOKIE_SECURE` value and is toggled independently via `HSTS_ENABLED`), so the example proxy configs do not add a second HSTS policy; set `HSTS_ENABLED=false` if you must keep secure cookies without pinning browsers to HTTPS for a year

Do not start from the base compose file and then expose `8080` publicly as a shortcut. The supported public path is the dedicated proxy stack where only the reverse proxy publishes host ports.

### Advanced knobs

These settings are valid, but they are not required for a safe first deployment:

- `TZ` and `DEFAULT_LANGUAGE` (`en`, `ru`, `es`, `fr`, `de`, `it`) for operator preference
- `DB_PATH` (default `data/ovumcy.db`) if you need the SQLite file somewhere other than the default inside the data volume. The bundled compose files already set it to `/app/data/ovumcy.db`; you only touch it for a non-standard layout or when running the binary or the operator CLI outside compose.
- rate-limit variables if you need stricter or looser local policy — every endpoint's default and its two variable names are tabulated in [docs/security/auth-policy-and-rate-limits.md](security/auth-policy-and-rate-limits.md)
- `AUDIT_LOG_ENABLED` (default `false`) if you want per-action security-event lines for incident investigation. They stay on the host and never leave it, but they carry `user_id` and role, so treat the resulting stream as the same sensitivity class as the database and plan retention for it. Full contract in [docs/security/logging.md](security/logging.md).
- optional OIDC variables when you want the login page to offer external sign-in: `OIDC_ENABLED`, `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` (or `OIDC_CLIENT_SECRET_FILE`, see below), `OIDC_REDIRECT_URL`, `OIDC_CA_FILE`, `OIDC_LOGIN_MODE`, `OIDC_RESPONSE_MODE`, `OIDC_AUTO_PROVISION`, `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS`, `OIDC_LOGOUT_MODE`, and `OIDC_POST_LOGOUT_REDIRECT_URL`. Leave `OIDC_RESPONSE_MODE` at its `form_post` default; set it to `query` only for providers that cannot form-post the callback (Dex, better-auth, Pocket ID <2.7) — the code then travels in the callback URL, inert without the PKCE verifier but logged by proxies. See [docs/oidc.md → Response mode](oidc.md#response-mode).
- `CALENDAR_FEED_FENCE_PATH` (the image sets `/app/fence/calendar-feed.fence`) only for a non-standard layout, or when you run the binary outside compose. What it is and why the location matters: [Calendar Feed Restore Fence](#calendar-feed-restore-fence)
- `PROXY_HEADER` (default `X-Forwarded-For`) — set it to the header your trusted proxy overwrites with the real client IP; the example stacks set `X-Real-IP`
- `DB_DRIVER=postgres` plus `DATABASE_URL=...` (or `DATABASE_URL_FILE=...`, see below) when you intentionally move the app runtime to Postgres, either through the bundled local/private Postgres stack or an operator-managed database service
- `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` (default `false`) only if you want the scheduled `ovumcy notify` webhook-reminder pass to refuse delivery to private/loopback/link-local targets (including RFC 6598 CGNAT `100.64.0.0/10`, `0.0.0.0/8`, `fec0::/10`, `64:ff9b:1::/48`, every IPv6 transition form wrapping a private IPv4 — NAT64, 6to4, Teredo, IPv4-compatible and IPv4-translated — and every other range the IANA special-purpose registries record as not globally reachable, such as RFC 2544 benchmarking space, the documentation prefixes and the reserved `240.0.0.0/4`); leave it unset for the common case of a self-hosted ntfy/Gotify instance on the same LAN. See [docs/notifications.md](notifications.md) for enabling and scheduling webhook notifications.
- `REMINDER_SCHEDULER_ENABLED` (default `false`) and `REMINDER_SCHEDULER_HOUR` (default `9`, local hour 0-23) only if you want the server process itself to run the daily webhook-reminder pass on a schedule, instead of (or in addition to) scheduling `ovumcy notify` externally. See [docs/notifications.md](notifications.md#the-built-in-daily-scheduler) for the full contract.

## Optional OIDC Sign-In

OIDC is supported in two public login modes:

- `OIDC_LOGIN_MODE=hybrid` keeps the local username/password flow alongside SSO;
- `OIDC_LOGIN_MODE=oidc_only` removes public local login, register, and forgot-password entry points from the browser UX.

[docs/oidc.md](oidc.md#current-contract) owns the account contract; what an operator has to decide here is:

- **A verified email claim never signs anyone in on its own.** Ovumcy uses an existing `(issuer, subject)` link when there is one; an email that merely matches an existing account sends the user to a confirmation step that demands that account's own password, plus its TOTP code when 2FA is on, before the link is stored. Nothing you can configure relaxes that — it is what stops a provider that can assert an email address from taking over the account.
- `OIDC_AUTO_PROVISION=true` may create a new `owner` account only when `REGISTRATION_MODE=open`, and `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS` narrows that to a domain allowlist.
- Auto-provisioned users start without a local password and must set one later in `Settings` if they want recovery codes or password-confirmed sensitive actions.

Operator checklist for OIDC:

- serve Ovumcy through HTTPS and set `COOKIE_SECURE=true`;
- set `OIDC_REDIRECT_URL` to the public HTTPS URL ending in `/auth/oidc/callback`;
- if the provider uses a private or internal CA, mount a readable PEM bundle into the container and point `OIDC_CA_FILE` at that in-container path;
- keep `OIDC_CLIENT_SECRET` private just like `SECRET_KEY` and `.env`; `OIDC_CLIENT_SECRET_FILE` takes a secret file path instead, the same pattern as `SECRET_KEY_FILE` below (`OIDC_CLIENT_SECRET` wins if both are set);
- prefer the dedicated reverse-proxy stacks for public deployments so the callback URL and cookie policy stay aligned;
- use `OIDC_LOGOUT_MODE=auto` when you want provider logout when available but do not want logout to break on providers that do not expose an end-session endpoint;
- use [docs/oidc.md](oidc.md) for provider-specific recipes, rollout guidance, flow details, and troubleshooting.

## Privacy Responsibility Split

Ovumcy itself provides:

- no analytics or third-party trackers in the core product;
- first-party cookies and sealed auth-related cookies;
- local SQLite storage under your deployment;
- documented backup/restore and proxy patterns that avoid leaking the plain app port publicly.

The self-hoster must still provide:

- host, VM, or NAS security and OS patching;
- TLS certificates, DNS, and reverse-proxy correctness for public access;
- access control for `.env`, backups, logs, and the persistent data volume;
- backup retention, off-host copy strategy, and recovery discipline;
- native Postgres backup/restore tooling and operational ownership if the advanced Postgres path is used, including the bundled local/private Postgres stack;
- network exposure policy, firewall rules, and any administrator access controls around the server.

## Reverse Proxy and HTTPS Contract

The supported reverse proxy path is intentionally narrow:

- TLS terminates at your own reverse proxy.
- The preferred public deployment path is a dedicated Docker bridge network where:
  - the `ovumcy` service has no published host port;
  - only the reverse proxy publishes `80/443`;
  - proxy-to-app traffic stays on the internal Docker network.
- Ovumcy continues to listen on plain HTTP at `:8080` inside that private network.
- `COOKIE_SECURE=true` is mandatory once the public site is HTTPS-only.
- `TRUST_PROXY_ENABLED=true` is valid only when every trusted proxy IP or internal proxy subnet is explicitly listed in `TRUSTED_PROXIES`.
  Each entry must be a literal IP in canonical form (`10.0.0.1`, `2001:db8::1`) or a CIDR range (`10.0.0.0/8`); with trust-proxy enabled,
  an entry the app cannot use refuses the boot, naming the rejected entry, rather than being dropped from the trusted set.
- A value this app cannot parse for `COOKIE_SECURE`, `HSTS_ENABLED`, `TRUST_PROXY_ENABLED` or `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` also
  refuses the boot, naming the key and the value — a typo in one of these no longer starts the instance on the insecure default.
  Accepted spellings are `1`/`true`/`yes`/`on` and `0`/`false`/`no`/`off`; leaving a key unset still means its documented default.
- Keep `PROXY_HEADER=X-Real-IP`; the example proxies set it to the real client IP. The app's own default is `X-Forwarded-For`, which the edge rate limiters handle safely — they key on the **rightmost untrusted** hop, so a spoofed prefix cannot defeat them, and the app prints an operator note at boot when trust-proxy is on with that header. What stays client-controlled under `X-Forwarded-For` is the leftmost entry that fiber's `c.IP()` returns, which feeds the secondary per-client auth-attempt buckets; the per-identity buckets that actually cap brute force are unaffected. A header your proxy overwrites gives you spoof-proof values everywhere.

The example stacks below use dedicated internal subnets and set `TRUSTED_PROXIES` to those exact ranges. If you adapt the stacks, keep the trusted proxy range as small as the network design allows. If the sample subnet collides with your environment, change both the Docker subnet and `TRUSTED_PROXIES` together.

## Reverse Proxy Examples

Use one of the example stacks as the supported public deployment path:

- Caddy, SQLite baseline:
  - Compose stack: [docs/examples/reverse-proxy/caddy/docker-compose.yml](examples/reverse-proxy/caddy/docker-compose.yml)
  - Proxy config: [docs/examples/reverse-proxy/caddy/Caddyfile](examples/reverse-proxy/caddy/Caddyfile)
- Nginx, SQLite baseline:
  - Compose stack: [docs/examples/reverse-proxy/nginx/docker-compose.yml](examples/reverse-proxy/nginx/docker-compose.yml)
  - Proxy config: [docs/examples/reverse-proxy/nginx/nginx.conf](examples/reverse-proxy/nginx/nginx.conf)
- Caddy, Postgres advanced path:
  - Compose stack: [docs/examples/reverse-proxy/caddy-postgres/docker-compose.yml](examples/reverse-proxy/caddy-postgres/docker-compose.yml)
  - Env template: [docs/examples/reverse-proxy/caddy-postgres/.env.example](examples/reverse-proxy/caddy-postgres/.env.example)
  - Proxy config: [docs/examples/reverse-proxy/caddy-postgres/Caddyfile](examples/reverse-proxy/caddy-postgres/Caddyfile)
- Nginx, Postgres advanced path:
  - Compose stack: [docs/examples/reverse-proxy/nginx-postgres/docker-compose.yml](examples/reverse-proxy/nginx-postgres/docker-compose.yml)
  - Env template: [docs/examples/reverse-proxy/nginx-postgres/.env.example](examples/reverse-proxy/nginx-postgres/.env.example)
  - Proxy config: [docs/examples/reverse-proxy/nginx-postgres/nginx.conf](examples/reverse-proxy/nginx-postgres/nginx.conf)

Both examples assume:

- the public hostname is `ovumcy.example.com`;
- you create a local `.env` file next to the example `docker-compose.yml` with at least `SECRET_KEY=...` or `SECRET_KEY_FILE=...`;
- the `ovumcy` service stays on a private Docker network and is not reachable directly from the host network;
- public traffic reaches only the reverse proxy.

If you choose `SECRET_KEY_FILE`, mount that file into the `ovumcy` container and use the in-container path in `.env`. The official compose examples include a commented bind-mount line for the common `/run/secrets/ovumcy_secret_key` path.

Prefer the Caddy stack if you want automatic certificate management. Use the Nginx stack if you already manage TLS certificates yourself and can mount them into `./certs/fullchain.pem` and `./certs/privkey.pem`.
Choose the SQLite baseline variants when you want the simplest public deployment. Choose the Postgres variants when you want the same proxy-only public exposure model with Postgres as the runtime engine.

## Official Local/Private Postgres Stack

If you want advanced self-hosted Postgres without building your own compose stack from scratch, use the bundled local/private example:

- Compose stack: [docs/examples/postgres/docker-compose.yml](examples/postgres/docker-compose.yml)
- Env template: [docs/examples/postgres/.env.example](examples/postgres/.env.example)

This path is intentionally narrow:

- it stays self-hosted and local/private;
- it publishes `http://localhost:8080` directly;
- it does not include HTTPS termination or a reverse proxy;
- it is meant to give advanced operators an official `ovumcy + postgres` runtime without mixing in public-internet proxy concerns.

Startup flow:

1. Copy the example `docker-compose.yml` and `.env.example` into a dedicated deployment directory.
2. Rename `.env.example` to `.env`.
3. Set a strong application secret via `SECRET_KEY` or `SECRET_KEY_FILE`, and set `POSTGRES_PASSWORD`.
4. Start the stack with `docker compose up -d`.
5. Confirm `docker compose ps` shows both `postgres` and `ovumcy` healthy.
6. Confirm `curl -fsS http://127.0.0.1:8080/healthz` succeeds.

This bundled stack is the recommended first step when you want Postgres but do not yet need a public reverse-proxy deployment path.

## Official Public Postgres Reverse-Proxy Stacks

If you want public self-hosted HTTPS and Postgres together, use one of the dedicated Postgres reverse-proxy stacks instead of splicing Postgres into the baseline proxy examples yourself:

- Caddy + Postgres: [docs/examples/reverse-proxy/caddy-postgres/docker-compose.yml](examples/reverse-proxy/caddy-postgres/docker-compose.yml)
- Nginx + Postgres: [docs/examples/reverse-proxy/nginx-postgres/docker-compose.yml](examples/reverse-proxy/nginx-postgres/docker-compose.yml)

These stacks follow the same [Reverse Proxy and HTTPS Contract](#reverse-proxy-and-https-contract) as the SQLite examples above, with `DB_DRIVER=postgres` and `DATABASE_URL` already wired and the proxy subnet already aligned with `TRUSTED_PROXIES`.

Use them when you need both:

- advanced self-hosted Postgres as the runtime engine;
- the preferred public self-hosted proxy-only exposure model.

## Health Checks by Deployment Mode

The app exposes two probes, and they answer different questions:

| Probe | Answers | Touches the database | Fails when |
| --- | --- | --- | --- |
| `GET /healthz` | Is the process alive? | No | The process is gone or wedged |
| `GET /readyz` | Can it actually serve requests? | Yes — one trivial query | Storage does not answer within one second |

`/healthz` deliberately never queries the database. That is what makes it safe as the container health check: a database that is slow for ten seconds, or a Postgres container restarting under the app, must not turn into a killed and restarted app container. It also means `/healthz` alone cannot tell you the app is *working* — it stays green with storage completely gone, which is exactly the case `/readyz` exists to catch.

`/readyz` runs one trivial query against the configured engine and answers `200` when it succeeds, `503` when it does not. Both responses are a fixed one-word JSON status — `{"status":"ok"}` and `{"status":"unavailable"}` — and neither reveals the engine, the database path, or the error. Reach for it when the container is healthy but the app is misbehaving, and use it as the drain signal in front of a load balancer.

The probe is bounded at **one second**, and it reports the same `503` whether storage is gone or merely too busy to answer in time — it deliberately cannot tell you which. Read a `503` on a container that is otherwise healthy as *"the app cannot serve right now"*, and check load before you go looking at the volume. On the SQLite baseline a handful of clients saving days at the same moment is enough to make the probe flap while ordinary requests still succeed, slowly (see [Concurrency on the SQLite baseline](#concurrency-on-the-sqlite-baseline)). The distinguishing signal is elsewhere: if requests are still completing — check the request log for `200`s with multi-second latencies — the storage layer is present and the answer is contention, not an outage.

The runtime image ships both as built-in subcommands, `ovumcy healthcheck` and `ovumcy readycheck`. Each makes an in-process request against `127.0.0.1:$PORT` and exits non-zero on failure, so the scratch-based container image needs no external HTTP client (no `curl`, no `wget`). Docker invokes `ovumcy healthcheck` automatically per the `HEALTHCHECK` directive baked into the image; `ovumcy readycheck` is yours to run on demand. The `HEALTHCHECK` directive is intentionally left on the liveness probe — do not repoint it at `/readyz` unless you actively want a database outage to restart the container.

Use the health check that matches your deployment path:

- Public reverse-proxy stack:
  - `docker compose ps` should show `ovumcy` as healthy;
  - `curl -fsS https://your-domain.example/healthz` should succeed through the proxy;
  - `curl -fsS https://your-domain.example/readyz` should succeed too — a `503` here with a healthy container means the app is up but its storage is not.
- Local/private base compose path:
  - `docker compose ps` should show the container healthy;
  - `curl -fsS http://127.0.0.1:8080/healthz` should succeed on the host;
  - `curl -fsS http://127.0.0.1:8080/readyz` should succeed on the host.
- Direct container probe (no host port published):
  - `docker exec ovumcy /app/ovumcy healthcheck` should exit `0`;
  - `docker exec ovumcy /app/ovumcy readycheck` should exit `0`.

### Running the operator CLI against the container

The runtime image is shell-free, so the operator subcommands are reached through `docker exec` on the binary itself. The probes above take no input and run as shown. The account subcommands (`users create`, `reset-password`) ask for a password, and how you invoke them decides how they read it:

```bash
# Interactive: prompts twice with echo disabled.
docker exec -it ovumcy /app/ovumcy reset-password owner@example.com

# Scripted: the password is the first line of stdin. Never pass it in the
# command line or an environment variable — both are visible to other
# processes and land in shell history.
printf '%s\n' "$NEW_PASSWORD" | docker exec -i ovumcy /app/ovumcy reset-password owner@example.com
```

Use the scripted form when you need to recover several accounts at once — for example after a `SECRET_KEY` rotation, where every 2FA-enabled owner needs a way back in.

**`reset-password` and `users delete` need the server's own restore fence.** Both remove calendar-feed access, and both confirm and advance the same fence the server uses before that removal is allowed to happen. Run them inside the container, where `CALENDAR_FEED_FENCE_PATH` and the mounted `ovumcy_fence` volume are the server's own — `docker exec` on the running container, or `docker compose run --rm ovumcy /app/ovumcy ...` with the same volumes when it is not up. From a host shell that cannot see that file they refuse and change nothing, naming the variable and what to run instead; an instance that has never started with a fence configured is refused too, because there is nothing yet to advance. See [Calendar Feed Restore Fence](#calendar-feed-restore-fence).

Every subcommand above applies any pending migrations first, exactly as the server does, so none of them runs on a database a migration is refusing. The one that does not is `ovumcy repair`, which exists for that case and opens the database without applying anything. It is also the subcommand you are most likely to reach with `docker compose run --rm` rather than `docker exec`, because the container it would attach to is usually down when you need it. See [Duplicate rows that refuse a migration](#duplicate-rows-that-refuse-a-migration).

For the public reverse-proxy stacks, do not treat a missing host-level `127.0.0.1:8080` listener as a problem. In the preferred deployment model, that port is intentionally not published to the host at all.

## Concurrency on the SQLite Baseline

The SQLite baseline is sized for a household, and it has a ceiling worth knowing before you meet it. SQLite allows one writer at a time. Reads scale fine — eight clients browsing the dashboard, calendar, stats and exports at once stay in the tens of milliseconds — but **concurrent writers serialize**, and a writer that cannot take the lock waits out the five-second busy timeout before the app retries.

Measured 2026-07-28 on the release image current that day (v1.9.2, tagged 2026-07-24) — mixed traffic with 40% day saves, per-request latency for `PUT /api/v1/days/{date}`. The numbers are a dated drill, not a guarantee; re-run them before sizing on a different host or a later release:

| Clients writing at once | Median save | 95th percentile |
| --- | --- | --- |
| 1 | 8 ms | 11 ms |
| 2 | 10 ms | 20 ms |
| 4 | 13 ms | 4.9 s |
| 8 | 5.0 s | 25 s |

Nothing breaks — there is no crash, no memory growth, no corruption, and the container keeps reporting healthy. Requests queue. What an operator sees is saves that take seconds, and `/readyz` flapping to `503` while ordinary pages still load.

So: one or two people saving entries at the same moment is comfortable. Four is the knee. If your instance regularly has more writers than that — several household members each on a phone and a laptop, or a scripted importer running alongside normal use — move to the Postgres path below. The same traffic at 24 concurrent clients stays at a 57 ms median save with no readiness flapping, because Postgres does not serialize writers.

This is a property of the storage engine, not a tuning knob: raising the connection pool does not help, since the constraint is the single write lock rather than the number of connections.

## Secret Handling and Rotation

Treat the application secret as part of the deployment identity, whether you pass it via `SECRET_KEY` or `SECRET_KEY_FILE`.

- `SECRET_KEY_FILE` should point to a readable path inside the running process or container. Trailing newlines are trimmed, but the secret still needs 32+ non-placeholder characters.
- `SECRET_KEY` takes precedence if both secret sources are configured.
- `DATABASE_URL_FILE` and `OIDC_CLIENT_SECRET_FILE` follow the identical pattern — a readable in-container path, trailing whitespace trimmed, the plain variable winning silently if both are set — for the Postgres DSN and the OIDC client secret respectively. This is the Docker Swarm/Compose secrets route for a runtime image that ships without a shell, so there is no `sh -c 'export X=$(cat …)'` workaround available.
- Store the underlying secret privately and back it up separately from the SQLite archive.
- Rotating the application secret invalidates existing sealed cookies and active sign-ins.
- Restoring SQLite data with a different application secret is valid, but users should expect a fresh sign-in and new sealed-cookie state.
- Rotating the secret on a database with TOTP-enabled accounts will leave their `users.totp_secret` ciphertexts undecryptable; affected users must recover at `/forgot-password` with **their account password *and* their recovery code** — the code replaces the broken second factor, never the password — and then re-enrol TOTP under the new secret. Someone who has forgotten the password has no self-service path back to it (a linked OIDC identity still signs in — the callback never consults TOTP — but an account that already has local auth is refused the local-password-setup step-up, so the password itself stays stuck) and needs the operator to run `ovumcy reset-password <email>`, which requires shell or container access — see [Running the operator CLI against the container](#running-the-operator-cli-against-the-container) — and, because a forced reset also revokes the account's calendar feed, a restore fence the command can confirm: through `docker compose exec` on the bundled stacks it already can, while a server that has never started with `CALENDAR_FEED_FENCE_PATH` configured has to be started once with it before the reset runs (see [Calendar Feed Restore Fence](#calendar-feed-restore-fence)).
- Rotation also breaks the **other** field-encrypted column, `users.webhook_url`. The daily reminder pass fails safe rather than delivering to a garbage target: it skips that owner (logging only the owner id) and keeps going for everyone else. Nothing surfaces in the UI, so reminders simply stop for those accounts until each owner re-saves their endpoint in Settings. It does not surface in the pass's own summary either — a skipped owner is not counted as a failure, so `ovumcy notify --dry-run` reports `failed: 0` and simply stops listing that owner under *would send*. When you verify a rotation this way, read the log lines above the summary (`webhook notify: decrypt failed, skipping owner id=N`) rather than the counters, and compare the *would send* block against one captured before the rotation.
- Rotation **disarms armed calendar (`.ics`) feeds**. The feed verifier is a keyed MAC derived from the application secret, and a MAC that no longer matches is refused outright — deliberately never re-checked against the row's older bcrypt hash. Subscribed calendar clients get `404` until each owner generates a fresh subscribe URL from Settings. Plan a rotation as a "re-issue the feed URLs" event, the same way you plan it as a "re-enrol TOTP" event. Feeds armed on versions before the MAC landed (pre-migration-032 rows) are disarmed by the first start under the new secret — the startup log prints `SECRET_KEY rotation detected: N legacy calendar feed(s) disarmed`. The first start after upgrading to a release that carries the sentinel disarms those legacy rows outright rather than adopting them as its baseline — nothing in such a row records which secret minted it, so a rotation performed **in that same maintenance window** would otherwise go unnoticed. It prints `calendar-feed key epoch recorded for the first time: N legacy calendar feed(s) predating the keyed MAC disarmed`, and owners whose subscription dates back that far re-generate their URL once. A new installation has no such row and the line stays absent. One sharp edge remains: starting the app with a mistyped `SECRET_KEY` counts as a rotation, permanently disarming any legacy rows still armed.
- See the *SECRET_KEY Usage Map* section in [SECURITY.md](../SECURITY.md) for the per-subsystem impact table these three bullets summarize.
- **If the secret is lost entirely** (no backup, no way to recover the old value), this is worse than a planned rotation: there is no key to roll forward from, so every `users.totp_secret` ciphertext is permanently unrecoverable — not just temporarily undecryptable — because the encryption key is derived from `SECRET_KEY` via HKDF with no escrow copy stored anywhere. All existing sealed cookies and sessions invalidate the same as with a rotation. A 2FA-enabled account can still recover on its own if it presents **both** its password and its recovery code, and then re-enrols TOTP. An account with `local_auth_enabled=false` (OIDC-only) also keeps a self-service path: its `(issuer, subject)` link is stored in plaintext and the OIDC state cookies are minted under the *current* secret, so a fresh provider sign-in still works — and it can mint a local password behind a fresh provider step-up (local-password setup refuses only accounts that already have one) and clear a dead TOTP enrollment itself. The accounts with no self-service path back in are the local-auth ones whose sign-in is blocked by the dead TOTP challenge, who cannot present password *and* recovery code at `/forgot-password`, and who have no linked OIDC identity to sign in through; each needs an operator to run `ovumcy reset-password <email>` to regain access — under the same restore-fence requirement as in the rotation bullet above. Treat total secret loss as a data-loss event, not a rotation, in your incident runbook.
- Do not paste the application secret, backup archives, or certificate material into issue trackers, chat logs, or shared shell history.

## Backup and Restore Contract

The supported self-hosted backup contract is intentionally narrow:

- Back up the SQLite data volume before every upgrade and before any manual recovery work.
- Treat every backup archive as sensitive health data.
- Keep `.env` and the application secret backup (`SECRET_KEY` value or the file behind `SECRET_KEY_FILE`) separate from the SQLite data archive.
- **Do not back up the `ovumcy_fence` volume, and never restore it alongside the database.** It is not data — losing it costs one round of re-generated calendar-feed subscribe URLs and nothing else — and restoring it together with the database is the one action that defeats it. See [Calendar Feed Restore Fence](#calendar-feed-restore-fence).
- Expect existing auth-related cookies to become invalid if you restore data with a different application secret.
- The SQLite database runs in WAL mode, so the data volume can also hold `ovumcy.db-wal` and `ovumcy.db-shm` next to `ovumcy.db`. The whole-volume archive flow below captures all three together — necessary, but on a running instance not sufficient. `tar` reads the three files one after another, and a checkpoint landing between the read of `ovumcy.db` and the read of `ovumcy.db-wal` writes the WAL into the main database file *after* that file was read, then empties the WAL *before* it is read: the archive carries all three files and is still missing a commit that was in the database before the backup began. Stop the app before you archive the data volume — or take the archive from an atomic snapshot of it rather than from the live volume. The same applies, for the same reason, if you copy individual files instead; stopping the app also checkpoints the WAL into the main database file.

For the bundled local/private Postgres stack, use native PostgreSQL backup tooling instead of the SQLite archive workflow:

```bash
mkdir -p backups
docker compose exec -T postgres sh -lc 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > backups/ovumcy-postgres.sql
```

Restore with the app stopped, the target database intentionally selected, and the existing schema dropped first:

```bash
docker compose exec -T postgres sh -lc 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" "$POSTGRES_DB" -c "DROP SCHEMA public CASCADE" -c "CREATE SCHEMA public"'

cat backups/ovumcy-postgres.sql | docker compose exec -T postgres sh -lc 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" "$POSTGRES_DB"'
```

The first command destroys everything currently in that database. Take a fresh rollback backup before running it if you are not already holding one you trust — the same precaution the [named-volume restore](#docker-named-volume-restore) states before `docker volume rm`.

Both additions are load-bearing:

- **Dropping the schema** is the Postgres equivalent of `docker volume rm ovumcy_data` + `docker volume create` on the SQLite path, and for the same reason: the restore has to start from an empty target. A plain `pg_dump` writes `CREATE TABLE` followed by `COPY` and carries no `DROP` of its own, so against a database that still holds its schema every `CREATE TABLE` fails — and `COPY`, one all-or-nothing statement per table, only lands in a table that is still empty.
- **`-v ON_ERROR_STOP=1`** makes `psql` treat those failures as failures. Its default is to run past every error and exit `0` regardless, which is what lets a restore that moved none of the data report success.

Without both, the restore's outcome depends on the state of the target database, and the three cases are indistinguishable from outside — all of them exit `0` after printing roughly 40 `ERROR:  relation "…" already exists` lines to stderr:

| State of the target database | What the documented command actually did |
| --- | --- |
| Tables empty (fresh volume, rehearsal stack) | Restored every row — the case that makes the procedure look correct |
| Partial loss (some rows survived) | Restored **nothing**; the missing rows stayed missing |
| Data intact, or drifted since the backup | Restored **no row**, and rewound every **sequence** to the value it held in the backup — `setval` is the one statement in a plain dump that succeeds against a populated schema. The operator is looking at the wrong generation of data on a database whose next insert lands on an id that is already taken |

With the drop step and `ON_ERROR_STOP=1`, the same restore prints no `ERROR:` lines, exits `0`, and brings back every table, row and sequence the dump carried.

Two differences to the dump you took survive it, and neither of them is your data, so do not read either as a failed restore:

- `pg_dump` 17.6 and newer wrap every dump in a fresh random `\restrict …` / `\unrestrict …` pair, so no two dumps of the same database are ever byte-identical.
- Dropping and recreating `public` leaves a schema that `initdb` did not create, so every dump taken *after* a restore carries an `ALTER SCHEMA public OWNER` / `COMMENT ON SCHEMA public` / `REVOKE USAGE ON SCHEMA public` block that the dump taken before it did not.

Compare two dumps by their `COPY` blocks and their `setval` lines rather than with a byte-for-byte `diff`.

A clean exit now means the two commands ran, not that the data came back: a truncated or empty dump replays into a freshly dropped schema without a single error and exits `0` just the same, leaving the stack healthy and blank. Finish through [Post-Restore Verification](#post-restore-verification) — `/readyz` and a normal page load stay green on an empty database, so neither closes out a Postgres restore either.

Keep the SQL dump and `.env` / application-secret backup separate, just as you would for the SQLite baseline.

The same rule applies to the public Postgres reverse-proxy stacks. Back up PostgreSQL with `pg_dump` or your platform-native Postgres snapshot tooling; do not try to apply the SQLite file-copy runbook to those stacks. The two restore requirements above — empty the target first, and run `psql` under `-v ON_ERROR_STOP=1` — are properties of `pg_dump` and `psql` rather than of the bundled stack, so they hold wherever you replay a plain dump.

Recommended baseline:

- Use the default Docker named volume when possible.
- Keep at least one recent rollback backup before replacing production data.
- Verify a restore by working through [Post-Restore Verification](#post-restore-verification), not by checking that the app is up. `/readyz` and a normal page load are worth running — a restore changes the storage layer, which is the half `/healthz` deliberately does not check — but they are steps 2-3 of that checklist, and every signal they cover stays green on an empty database. Reading data back is the step that decides whether to trust the restore.

Bind mounts are still valid, but they are an advanced operator path. For bind mounts, stop the app and back up the mounted directory with normal filesystem tools while preserving file contents and access permissions.

## Docker Named Volume Backup

The default compose deployment uses the `ovumcy_data` named volume.

Stop the app first — `docker compose stop`, then `docker compose start` once the archive is written — or take the archive from an atomic snapshot of the volume. Archiving the live volume runs the checkpoint race described in [Backup and Restore Contract](#backup-and-restore-contract): the archive comes back carrying all three WAL-set files and missing a commit that was already in the database when it started, and nothing about the run reports a problem.

A portable manual backup flow is:

```bash
# Stop the app first, or take the archive from an atomic snapshot of the volume:
# archiving the live volume can drop a commit that was already in the database,
# and the run reports nothing. The stop/start commands are in the paragraph
# above this block, under "Docker Named Volume Backup".
mkdir -p backups
BACKUP_FILE="ovumcy-data-backup.tgz"

docker run --rm \
  -e BACKUP_FILE="$BACKUP_FILE" \
  -v ovumcy_data:/source:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.24.1 \
  sh -c 'cd /source && tar czf "/backup/$BACKUP_FILE" .'
```

This archive contains sensitive user data. Store it like a secret, not like an ordinary log file.

## Docker Named Volume Restore

Use this restore flow only when you have already stopped the app and confirmed which backup archive should replace the current data:

```bash
BACKUP_FILE="ovumcy-data-backup.tgz"

docker compose down
docker volume rm ovumcy_data
docker volume create ovumcy_data

docker run --rm \
  -e BACKUP_FILE="$BACKUP_FILE" \
  -v ovumcy_data:/target \
  -v "$PWD/backups:/backup:ro" \
  alpine:3.24.1 \
  sh -c 'cd /target && tar xzf "/backup/$BACKUP_FILE"'

docker compose up -d
```

Before removing the existing volume, make a fresh rollback backup if you are not already holding one you trust.
When you restore into a manually recreated named volume, Docker Compose may print a warning that the volume was not created by Compose. In this workflow that warning is expected and does not by itself mean the restore failed.
After startup, verify the restored app using the health check appropriate for your deployment mode, then work through [Post-Restore Verification](#post-restore-verification) below. The health check tells you the app is up; it cannot tell you the data came back.

## Post-Restore Verification

Steps 1-3 below check that the app is **up**. Steps 4 and 5 are the ones that check your **data came back** — step 4 that there is data at all, step 5 that it is the data from the backup — and between them they catch the two failures this procedure would otherwise hide.

The first is an archive that missed the database: it restores into an instance that boots cleanly, reports healthy, answers `/readyz` with `200`, and renders a perfectly ordinary login page. The usual way to end up with one is a per-file copy of `ovumcy.db` taken while the app was running — on an instance that has not yet checkpointed, nearly the whole database still lives in `ovumcy.db-wal` and the main file is close to empty.

The second is a restore that never ran. It has no empty-looking symptom at all: the instance is healthy and fully populated, because nothing was replaced. This is the Postgres shape — a `psql` restore into a database that still holds its schema can leave every row exactly as it was and still exit `0` (see [Backup and Restore Contract](#backup-and-restore-contract)) — and step 4 passes on it, because the records it asks about are indeed there. Do not stop at step 3, and do not stop at step 4.

After restore:

1. Confirm the container becomes healthy.
2. Confirm `/healthz` **and** `/readyz` respond successfully using the health check appropriate for your deployment mode.
3. Open the main UI once and verify the app renders normally.
4. Sign in and confirm the records are there: open the calendar on a month you know had entries before the backup, or download `Settings → Export` and compare it against an export taken before the restore. Do this even when the app looks perfectly healthy — every signal above stays green on an empty database.
5. Confirm the records are the ones **from the backup**, not the ones that were already in place. Before starting the restore, name a signal that is known to differ between the current database and the backup — the entry count for a month you edited since the dump was taken, a specific entry that exists in only one of the two — and check that signal afterwards. Phrase it so it can fail: "the calendar still shows entries" passes on a restore that did nothing, while "July shows 12 entries, not the 3 it showed an hour ago" does not. If you cannot name a differing signal, take a `Settings → Export` immediately before the restore and diff it against one taken after; an export that comes back unchanged after restoring a different generation of data is the failure, not a reassurance.
6. If you restored with a different `SECRET_KEY`, expect existing auth sessions and sealed cookies to be invalid and require a fresh sign-in. Read that expectation carefully against step 4, because the two failures look similar for one screen and mean opposite things: with a changed key your **password still works** (password hashes do not depend on `SECRET_KEY`) and you are merely signed out — 2FA is the part that breaks, and the recovery path for it is in [Secret Handling and Rotation](#secret-handling-and-rotation). A sign-in that is rejected as *wrong credentials* is not a key symptom at all; it means the account is not in the restored database, which is step 4 failing.
7. Expect every armed calendar feed to be gone, and tell the owners on this instance to re-generate theirs from `Settings → Calendar feed`. A restore returns the feed columns to their state at backup time, so a subscription an owner revoked or rotated *after* that backup was taken would otherwise come back live at its old URL; the [restore fence](#calendar-feed-restore-fence) disarms all of them on the boot that follows instead, before the instance accepts a request. Read the startup log to confirm it did: a line naming the disarmed count is the fence working. A line saying the fence is *unavailable* means it disarmed for a different reason — it has nowhere to keep its marker — and that it will keep disarming on every start until you mount one. Only the account itself can arm a feed again: every account is the sole owner of its own data, so there is no operator surface that regenerates another owner's subscription. Detail: [docs/gdpr.md → Backup Restore and the Calendar Feed](gdpr.md#backup-restore-and-the-calendar-feed).
8. If the backup you restored predates a `clear-data` or account deletion, that erasure did not happen in the restored database — the records it was meant to remove are back, exactly as calendar-feed columns are. `clear-data` and account deletion have no server-side memory of having run once outside the rows they touched, so nothing in the restored data flags that an owner asked to erase it. Check the operator's own request log or ticket history for any clear-data/delete-account request timestamped after the backup, and re-apply it manually if one exists. Detail: [docs/gdpr.md → Backup Restore and Erasure](gdpr.md#backup-restore-and-erasure).

## Calendar Feed Restore Fence

An owner who revokes their `.ics` calendar feed expects the old subscribe URL to be dead for good. A database restore would undo that on its own: the feed columns come back exactly as the backup holds them, and nothing in the restored rows records that a revocation ever happened.

Ovumcy closes that itself. It keeps a marker in two places — one inside the database, one in a file at `CALENDAR_FEED_FENCE_PATH` — and advances both together on every change to the set of armed feeds. A restore rolls back only the copy inside the database, so the two disagree, and the instance answers that on its next boot by disarming every armed calendar feed before the listener starts. Owners re-generate their subscribe URLs; nothing that was revoked comes back.

The whole mechanism rests on where that file lives:

- **It must be outside whatever your database backups capture.** The bundled compose stacks mount a separate `ovumcy_fence` volume for it, which the documented backup and restore commands never touch. A fence kept inside the data volume comes back with the database, agrees with it, and detects nothing.
- **Never back it up, and never restore it.** It carries no health data and no secrets — it is an opaque marker — and losing it costs one round of re-generated subscribe URLs. Restoring it beside the database is the one action that defeats the fence.
- **Nothing else depends on it.** Feeds are all it protects, so losing the fence volume disarms feeds on the next start and touches nothing else.
- **Run the operator CLI where the fence is visible.** `ovumcy reset-password` and `ovumcy users delete` both revoke calendar-feed access, so before writing anything they confirm that the two markers agree and advance both — the same two halves the server's own writes advance — and refuse when they cannot be confirmed; never merely warn. They can do that wherever `CALENDAR_FEED_FENCE_PATH` names the server's own fence file: through [`docker compose exec`](#running-the-operator-cli-against-the-container) on the bundled stacks it already does, and a binary run outside compose is pointed at the same path the server runs with. It has to be the server's file itself. A copy of it is not the fence: the command would confirm and advance the copy, the server's own file would never move, and a later restore would agree with that untouched file — reviving exactly what the command removed. Wherever the command cannot confirm the fence — the variable unset, a relative path, no volume behind it, markers that disagree, or a server that has never started with the fence configured — it refuses, names the variable and the remedy, and changes nothing. That last case changes an old habit: on a server that has never run with `CALENDAR_FEED_FENCE_PATH`, a forced `reset-password` is refused too, so configure the fence for the server and start it once before recovering an account through the CLI.
- **The CLI and a running server share no lock, deliberately.** A backup taken in the sub-second window between the CLI confirming the fence and writing its own advance is not a case either process guards against; the worst outcome is one extra disarm-all on the next start, which is cheaper than serializing every operator command against the running server. Run operator commands while the application is stopped when you can, and otherwise accept that an unluckily timed backup costs the affected owners a round of re-generated subscribe URLs.

What happens when:

| Situation | What the instance does |
| --- | --- |
| Ordinary restart | Both markers agree. Nothing is disarmed, nothing is logged. |
| First start of a new installation, or the first start after upgrading to the release that added the fence | The marker is written to both places. Nothing is disarmed — an upgrade is not a restore, and armed feeds keep working. |
| A database restore, or a fence volume that was recreated | Every armed calendar feed is disarmed, and the startup log names the count. |
| No fence available at all (no mount, `CALENDAR_FEED_FENCE_PATH` unset, or the path not writable) | Every armed calendar feed is disarmed **on every start**, and the startup log says so and names the variable. The instance still boots and everything else works; the calendar feed is effectively unavailable until you mount a fence. The database also records that this instance started without a fence, which is what the row below reads. |
| First start **with** a fence on a database that had already started **without** one | Every armed calendar feed is disarmed once, and the startup log says the fence was armed for the first time on a database that had run without it. Expected, not a fault — see the paragraph after this table — and the starts after it disarm nothing. |
| `CALENDAR_FEED_FENCE_PATH` set to a relative path | The instance refuses to start — the one row in this table where it does not boot. See the paragraph after this table. |
| `ovumcy reset-password` or `ovumcy users delete` is run | Before writing anything, the command confirms the two markers agree and advances both, then reports where it advanced them. Wherever it cannot confirm that, it refuses and changes neither the fence nor the account. |

**Adding the fence to an instance that has been running without one costs one round of subscribe URLs, on purpose.** While no fence was available, nothing outside the database recorded that an owner revoked or rotated a feed — so a backup taken in that period is indistinguishable from the database it would replace, and restoring it puts every feed back at the URL it had. The instance stamps its database on each such start, and the first start that finally has a working fence reads that stamp, disarms every armed feed once, and then behaves normally. Expect it, tell your owners to re-generate their subscribe URLs from `Settings → Calendar feed`, and read the startup line to confirm it is that case and not a restore. The same applies to a backup you took before mounting the fence: restoring it later disarms everything on the next start. That is containment, not data loss — a calendar feed is regenerated in one click by its owner, and nothing else in the backup is affected.

If you run the binary outside compose, point `CALENDAR_FEED_FENCE_PATH` at a file in a directory your database backups skip, and use an absolute path: the server and the operator CLI each resolve a relative one against their own working directory, which are not guaranteed to be the same. Both refuse a relative path rather than resolve it — the server stops at startup with an error naming the variable and the value it was given, the same value the CLI already refuses. If you deliberately do not use the calendar feed you can leave it unset on the server — the fence then has nothing to protect, and the startup line is its only effect there — but `ovumcy users delete` and a forced `ovumcy reset-password` still need one: with no marker recorded anywhere, both refuse instead of completing until a server has started at least once with a writable fence configured, because there is nothing yet for either command to confirm.

## Safe Upgrade Procedure

Use this sequence for routine upgrades:

1. Confirm you know where the persistent volume or bind mount is stored.
2. Take a backup of the database before changing the image version.
3. Pull the new image and restart the service.
4. Wait for the container healthcheck to report healthy.
5. Confirm `/healthz` through the correct deployment-mode health check and open the main UI once to confirm the app is responding.
6. If the new version fails to start cleanly, roll back to the previous image tag and restore from backup if needed. Confirm such a restore through [Post-Restore Verification](#post-restore-verification) rather than by repeating steps 4-5: the container healthcheck and the main UI stay green on an empty database, so they say the rollback booted, not that the data is back.

Migrations apply automatically on every boot: there is no `-migrate` flag or manual step to run (unlike tools such as Miniflux). Starting the new binary or image runs any pending embedded migrations against the database before the server accepts traffic, in order, forward-only, with no down-migration path — this is why step 2 (backup before restart) is mandatory rather than optional.

Practical Docker flow for the local/private base compose path:

```bash
docker compose pull
docker compose up -d
docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
```

If you changed `HOST_BIND_ADDRESS` or `PORT`, adjust the host-side health-check URL accordingly.

For the public reverse-proxy example stacks, run the same `docker compose pull`, `docker compose up -d`, and `docker compose ps` sequence inside the example directory, then verify `https://your-domain.example/healthz` through the proxy instead of expecting a host-level `127.0.0.1:8080` listener.

Keep `OVUMCY_IMAGE` on a concrete release tag and update it intentionally during upgrades instead of relying on a floating `latest`.

### Downgrade Caveats

Migrations are forward-only. There is no `down.sql` and no automated rollback path.

**From v2.0.0 on, a downgrade is refused rather than allowed to run quietly.** A start whose database records migrations the binary does not carry ends with `refusing to start against a newer schema: the database records migration(s) … as applied`, naming them and the newest one the binary knows. Nothing is executed and the database is untouched: start the newer release again, or restore a backup taken before the upgrade. This is the check the older binary was missing — it applied nothing, logged nothing, and served requests against columns and conventions it did not know about. Note where the check lives: **in the binary being started**. Going down to a release that predates v2.0.0 is therefore still silent, because that binary has no such check — for that direction the entries below are all the protection there is.

An additive migration (`ALTER TABLE ... ADD COLUMN ... NOT NULL DEFAULT ...`, `CREATE TABLE IF NOT EXISTS ...`) is otherwise harmless in itself: an older binary ignores columns and tables it does not know. The cases that warrant operator attention are:

- **Migration `019_canonicalize_date_fields_utc.sql` (v0.6.x baseline)** rewrites `daily_logs.date` and `users.last_period_start` so the stored values are UTC-midnight. Versions newer than 019 assume that invariant in their calendar/range queries. If you downgrade to a binary that predates 019 and then continue writing data, the new (older) binary may persist non-canonical timestamps; a subsequent re-upgrade will then see a mix of canonical and non-canonical rows and calendar views can drift by a timezone offset until the rows are normalized. Treat a downgrade past 019 as a one-way operation unless you also restore the database from a pre-019 backup.

- **Migration `022_register_pickup_tokens.sql` (v0.9.5)** introduces server-side single-use tracking for the `ovumcy_register_pickup` cookie. A downgraded binary that predates 022 still issues registration pickup cookies but does not insert rows into `register_pickup_tokens`. If you then re-upgrade, new registrations issued by the older binary cannot be exchanged on the welcome endpoint — the user falls through to `/login` and must re-register or sign in normally. This is a UX trade-off, not a security regression.

- **The one-shot luteal-phase recompute** is not a migration but changes stored data on the same boot, so it belongs in the same list. The personalized luteal-phase estimate in `users.luteal_phase` used to be measured one day too long, which moved the predicted ovulation date and both fertile-window edges a day earlier for owners who had logged enough BBT or cervical-mucus signal to earn one. The first boot of the corrected version recomputes that column from each owner's own logs and records a marker in `app_state` once it has been through every account without a failure, so it normally runs exactly once; an account it could not read or write is skipped and picked up by the next boot, and the startup line reports how many. Nothing an owner typed is touched: the column has no input surface and is derived from the day logs alone. A downgrade does not restore the previous values and does not need to — the older binary re-derives the column under its own convention the next time the owner saves a day or restores an export, and until then the only difference is a prediction one day apart. Restore from a pre-upgrade backup only if you need the previous values back immediately.

- **Migrations `032`, `034` and `035` are plain additive schema changes** — a nullable calendar-feed verifier column beside the one it replaces, an interface-language column defaulting to "never chosen", and a column DEFAULT restated on PostgreSQL only. An older binary ignores all three and keeps working; `032` in particular leaves the previous `calendar_feed_verifier_hash` column in place and still written, precisely so a rollback keeps verifying every existing feed token. Nothing to do in either direction.

- **Migration `033_purge_unattributed_oidc_logout_states.sql` deletes rows**, the only migration in this range that does. They are pre-031 OIDC logout states with a NULL `user_id`, which account deletion could never reach and which expire on their own within 7 days. A downgrade does not bring them back and does not need them: nothing reads a logout state after its TTL, and their absence is the point of the migration. No owner data is involved.

- **Migration `036_secret_reveal_consumption_marks.sql` is additive, but a downgrade turns off what it enforces.** The columns record that the recovery code and the calendar-feed subscribe URL have been shown once. A binary that predates 036 does not write them, so while you are downgraded a client that kept the sealed reveal cookie can be shown either secret again. Re-upgrading resumes recording from that moment; it cannot know about a reveal that happened while the older binary was running. Treat a downgrade past 036 as a window in which "shown exactly once" is not enforced, and rotate the calendar feed afterwards if that matters to the owners on the instance.

- **Migration `038_webhook_config_version.sql` is additive with the same shape of gap.** The counter lets a reminder pass tell a stale delivery snapshot from the current one. An older binary does not bump it, so while you are downgraded a notify pass can still deliver against a configuration the owner changed mid-pass. The column keeps its value across the downgrade and the protection resumes on re-upgrade.

- **Migration `037_symptom_name_uniqueness.sql` is downgrade-safe and upgrade-fragile, which is the reverse of the entries above.** The index it adds is simply ignored by an older binary — except that the index is still enforced by the database, so a duplicate name an older binary would have answered with "symptom name already exists" comes back to it as an unrecognized constraint error instead. Going the other way is where it bites: if the database already held two symptoms one account had under the same name, the upgrade refuses this migration and the instance does not start. That is deliberate and loses nothing, and it has its own runbook — [Duplicate rows that refuse a migration](#duplicate-rows-that-refuse-a-migration).

- **Migration `039_delivery_mark_and_feed_key_epoch.sql` is additive and carries two independent columns.** One records when a webhook delivery was actually accepted, the other pins the key regime a calendar-feed token was minted under. An older binary writes neither: while you are downgraded, delivery times stop being recorded, and a feed token minted by the older binary carries no epoch, which the re-upgrade reads as a token from an unknown regime and disarms on its next start. Nothing is lost that an owner typed; the owners affected re-generate a subscribe URL once.

- **The calendar-feed key-epoch sentinel is not a migration and does not roll back.** The first start of a release that carries it disarms every armed feed row minted before migration 032 (see [Secret Handling and Rotation](#secret-handling-and-rotation)). A downgrade does not bring those subscriptions back — the rows were cleared, not hidden — and the owners re-generate a subscribe URL from Settings on whichever version they end up on.

If you need to downgrade through a migration above, restore the database from a backup taken before that migration was applied. An entry that is a one-shot pass rather than a migration states its own remedy, which is weaker — its bullet says whether a restore is needed at all. Keep an "upgrade-paired" backup file alongside the image-tag bump in your runbook so a rollback in either direction is a single restore-from-backup step.

## Duplicate rows that refuse a migration

A migration that adds a **unique** index checks first, and refuses if the database already holds rows the index cannot cover. It names every conflicting group, writes nothing, rolls its transaction back, and ends the start. That refusal is the safe outcome — the only ways to make room would be to delete or merge somebody's rows, and a schema change is not consent to do either — but it does mean the instance stays down until you resolve it.

The startup log line looks like this:

```
refusing migration 037_symptom_name_uniqueness.sql: table symptom_types already
holds rows that unique index idx_symptom_types_user_name_unique on (user_id,
lower(name)) cannot cover ...
```

Resolve it **offline**. There is no running application to fix the data in — this refusal is what stopped it — and every other operator subcommand opens the database the same way and meets the same refusal. The `repair` subcommand is the exception: it opens the database without applying migrations, so it can reach exactly the state that is stuck.

`ovumcy repair` on its own lists the repairs the binary carries. Each one inspects and reports by default and changes nothing until `--apply`.

### Repairing duplicate symptom names

1. **Stop the service.** The container restarts on its own otherwise, and a repair must not run beside a boot attempt.

```bash
docker compose stop ovumcy
```

2. **Back up the database.** Follow [Docker Named Volume Backup](#docker-named-volume-backup). This step is not optional: the repair removes rows, and there is no undo other than this archive.

3. **Inspect.** This changes nothing and prints the plan — which symptom each account keeps and which rows fold into it:

```bash
docker compose run --rm ovumcy /app/ovumcy repair symptom-names
```

It exits non-zero while duplicates remain, so an upgrade script can gate on it. Read the plan before the next step.

4. **Apply.** For each group the kept symptom absorbs the others: every day log that named an absorbed symptom is re-pointed at the kept one first, in the same transaction that removes the rows, so no day loses what the owner recorded and a day that named both ends up naming one:

```bash
docker compose run --rm ovumcy /app/ovumcy repair symptom-names --apply
```

5. **Start the service** and confirm the migration applied:

```bash
docker compose up -d
docker compose logs ovumcy | tail -n 40
```

Running the repair a second time finds nothing and reports so, which is also what a partly-failed run leaves behind — the merge is one transaction, so a failure changes nothing and can simply be repeated.

What the repair does **not** do: it never edits a name, and it never chooses between two different symptoms. It only collapses rows an account holds under the *same* name, which is a state a normal request could never have produced. The kept row is the one still reachable by the owner — an active row before an archived one, a built-in before a custom one, and the oldest of what remains. That order matters because a built-in symptom cannot be renamed, hidden or removed from Settings, so leaving a second built-in behind would leave the owner a duplicate in the day-entry picker with no way to clear it.

One caveat if you ever move a SQLite instance to Postgres. The index folds names with the database's own `lower()`, and so does this repair — which is what makes one command correct on either engine. But the two engines do not fold alike: PostgreSQL folds by locale, SQLite folds ASCII only. Two symptom names differing only in the case of a non-ASCII letter are therefore one name to PostgreSQL and two to SQLite, so a database this repair reports clean on SQLite can still refuse the migration once its rows are in PostgreSQL. Run the inspection again after the import, before you conclude the move went through.

For the Postgres stacks, run the same commands from the example stack's directory; `docker compose run` starts the database container the app depends on.

If you run the binary outside compose, the command is the same with the same `DB_DRIVER`/`DB_PATH` (or `DATABASE_URL`) environment the server uses. It needs no `SECRET_KEY` and does not touch the calendar-feed fence. Get the path wrong and it says so, naming the database it actually opened — worth knowing because SQLite does not refuse a path that is not there, it creates an empty database at it.

## Troubleshooting Baseline

Use this order when something looks wrong:

1. Check container state:

```bash
docker compose ps
```

2. Check container logs:

```bash
docker compose logs --tail=200 ovumcy
```

3. Check the health endpoint that matches your deployment mode:

```bash
# Public reverse-proxy stack
curl -fsS https://your-domain.example/healthz

# Local/private base compose path
curl -fsS http://127.0.0.1:8080/healthz
```

4. If the public reverse-proxy URL fails but `docker compose ps` shows `ovumcy` healthy, inspect the proxy configuration, certificate mounts, and DNS first.
5. If the app is not healthy, inspect environment variables, permissions on the persistent volume, and the current image tag before changing application data.
6. For the Postgres reverse-proxy variants, also confirm `docker compose ps` shows `postgres` healthy before debugging proxy behavior.

Typical failure split:

- App issue: container exits, the container healthcheck fails, or `/healthz` fails inside the intended deployment path.
- Config issue: startup logs show invalid env values or trusted-proxy configuration errors. Most invalid values fall back to the
  documented default and the container keeps running; an unparseable security toggle or `TRUSTED_PROXIES` entry exits instead, with
  the last log line naming the key and the rejected value.
- Proxy issue: `ovumcy` is healthy, but public requests fail, loop, or lose the real client IP.

### `431 Request Header Fields Too Large`

This is per-browser, not per-deployment: the same page loads normally in one browser and is rejected
in another, because the limit is reached by the cookies that browser has accumulated for the domain.
Clearing them fixes it immediately, which is also how you confirm the diagnosis.

The whole **head** of a request — start line plus every header, cookies included — must fit in a
4 KB read buffer.

A normal signed-in request carries about **450 B** of Ovumcy cookies (measured from the running app
on 2026-07-25, against v1.9.2: `ovumcy_auth` 350 B, `ovumcy_csrf` 55 B, plus the language and
timezone cookies). Cookie payloads change with the code, so treat every byte count in this section
as that dated measurement rather than a current fact. The largest single
cookies are `ovumcy_oidc_stepup` at 514 B, `ovumcy_reset_password` at 460 B and `ovumcy_auth` — the
three that carry a signed token or a password hash.

Summing **every** cookie the app can define reaches roughly 2.9 KB, which with a browser's own
0.6–1.2 KB of headers would sit close to the limit. That total is arithmetic rather than a reachable
state: the transient cookies in it are mutually exclusive by lifecycle — a password-reset cookie and a
2FA-setup cookie never coexist — and the four OIDC cookies are each `Path`-scoped to the one
endpoint that consumes them, so none of them rides on an ordinary page request at all:
`ovumcy_oidc_auth` and `ovumcy_oidc_stepup` on `/auth/oidc/callback`, `ovumcy_oidc_link_pending`
on `/auth/oidc/link-confirm`, and `ovumcy_oidc_logout_bridge` on `/auth/oidc/logout`. Ovumcy on
its own therefore stays far below the buffer in every state a real flow produces, but the margin comes
from those exclusions, not from a large absolute gap.

It becomes reachable when **something else on the same domain** contributes cookies or headers:
analytics, a CDN or bot-management cookie such as Cloudflare's `__cf_bm`, another application sharing
the registrable domain, or a proxy that injects a large header block. Those ride along on every
request to Ovumcy and consume the same budget.

**What you will see.** The client receives `431` carrying the standard error envelope with the stable
key `request_headers_too_large` — so a browser shows the JSON body rather than a styled page, since
the request never reached the point where a page could be rendered. The server logs one explicit line:

```
request rejected: 431 request header fields too large — the request head did not fit the server read buffer
```

Note the accompanying request-log entry reads `404 | GET | /`, not `431`. That is not a second
problem: the head never parsed, so the request carries no method or path by the time the request
logger runs. The explicit line above is what ties the user's `431` to the server side — without it the
rejection is invisible among ordinary not-found noise.

Confirm the cause by having an affected user clear cookies for the domain, or open a private window:
if the request then succeeds, the head was over budget. Serving Ovumcy on its own hostname, rather
than sharing one with other cookie-setting services, keeps it clear of the limit.

Raising the header buffer on your reverse proxy will not help: the example stacks leave the proxy at
its own defaults, which are more generous than the app's, so a request that the proxy accepts can
still be refused behind it. The fix is on the cookie side, not the proxy side.

### An account cannot sign in after upgrading: email stored in a legacy form

Registration and sign-in used to accept a full RFC 5322 form (`jane doe <jane@example.com>`) and store it verbatim; sign-in input is now normalized to the bare address only, so such stored rows would never match again. The first boot after upgrading repairs them automatically: each is rewritten to its bare parsed address, and the startup log reports `auth email repair: N stored email(s) rewritten to their bare address`.

Two cases are left untouched, counted in the same log line, and cannot sign in until you repair them:

- another account already answers to the same bare address (two accounts on one mailbox — previously possible because the duplicate check compared stored forms): the oldest account keeps the address, and the later one is the leftover;
- the stored value cannot be reduced to a plain address at all (for example a quoted local part).

**Repair by id, never by address.** `ovumcy users list` prints the id beside the stored value, and the id is the only handle that reaches such a row: the stored string is a form sign-in normalization refuses outright, so no address-taking command accepts it, and its bare address resolves the *other* account — the one that kept the address. A `users delete` typed with that address in front of you would erase the wrong account's entire health record, and a `reset-password` typed with it would reset a stranger's password instead of the locked-out account's. `reset-password --id <id>` reaches the leftover row the same way `users set-email --id` and `users delete --id` do, and refuses (naming the ids) if a bare address you try instead still matches more than one row.

```bash
docker compose exec ovumcy /app/ovumcy users list
docker compose exec ovumcy /app/ovumcy users set-email --id 7 jane.doe@example.com
```

`set-email` moves exactly that one account, and nothing else. The new address is validated by the same rule a sign-in input is normalized under (a bare address, no display name or angle brackets), it is refused if another account already answers to it, and it is written under a compare-and-set on the value `users list` showed — so a row that changed in between is reported instead of overwritten. The account's health record is untouched. Its active sessions are all signed out, because the address *is* the login identity, and the owner signs in with the new address and their existing password. Confirm the repair the same way you found it: `ovumcy users list` shows the new address, and the owner can sign in.

Delete only an account that is genuinely surplus, and delete it by id too — the confirmation then quotes the exact stored address, id and role before anything is erased:

```bash
docker compose exec ovumcy /app/ovumcy users delete --id 7
```

That erasure is permanent and takes the account's whole health record with it; there is no undo short of restoring a backup. The command also revokes the account's calendar feed, so it confirms the server's own restore fence before it deletes anything — the [`docker compose exec`](#running-the-operator-cli-against-the-container) form above already reaches it; a server that has never started with `CALENDAR_FEED_FENCE_PATH` configured refuses this command too, until it has been started once with a writable fence.

On an OIDC instance, an account is matched to the provider by its stored address only until its first sign-in links issuer and subject; after that the link resolves it. Re-homing an account that is already linked therefore keeps its OIDC login working. The old address becomes free — and with auto-provisioning enabled, a later sign-in under it creates a **new empty account** rather than reaching the re-homed one.

### The server exits during startup instead of coming up

Read the last line first: a start that ends with `refusing migration ...` is not one of the cases below. That is a migration declining to run against data it cannot cover, before any of these passes exists, and it is resolved with the instance down — [Duplicate rows that refuse a migration](#duplicate-rows-that-refuse-a-migration).

Three passes run on every start, after the migrations and before the server begins serving: the calendar-feed key-rotation check, the auth-email repair described above, and the luteal-phase recompute. Each pass is allowed **five minutes** end to end — not per query, so a pass making many individually fast queries can still reach it — after which a database that accepts the connection and then stops responding ends the start with an error naming the pass, instead of leaving the process alive, silent and not listening — which is what it used to do, and which a container healthcheck can only ever report as "starting".

The log line names the pass and carries the storage error underneath it — typically `context deadline exceeded`, though the exact wording comes from the database driver and a Postgres server cancelling an in-flight statement may word it differently.

Five minutes is roughly two orders of magnitude above what these passes cost on a healthy instance, so reaching it means storage is stuck rather than slow. Check the database first: for SQLite, whether another process still holds the file (a stray container, a backup job, a copy in progress); for Postgres, whether the endpoint is accepting connections and then stalling.

A start cut short this way loses nothing, but not because the pass stopped cleanly — it stops wherever it was, and the accounts it had already reached stay changed. What makes that safe is that every change a pass makes is one it would make again, and the marker that would stop it from running next time is written only after a complete pass. So the interrupted pass simply runs again on the next start, re-reaches the rows it already fixed, agrees with them, and finishes the rest.

Two of the three end the start on failure, deliberately: a calendar feed left armed after a key rotation, or an account left unable to sign in, is worse than an instance that refuses to come up. The luteal-phase recompute does not — it logs `luteal-phase recompute failed … (retried on the next start)` and lets the server start, because it maintains a derived cache with a safe fallback. A count that repeats across starts there is a durable fault worth investigating rather than a transient one to wait out.

Because the budget is per pass and the passes are sequential, the worst case for the start as a whole is fifteen minutes. Size a container healthcheck start period or a deployment timeout against that, not against five.

## Common Operator Scenarios

- Moving from local/private to public HTTPS:
  start from the dedicated Caddy or Nginx example stack, then migrate your existing SQLite volume into that stack instead of exposing the base compose app port directly.
- Changing the proxy subnet or host:
  update the Docker subnet or proxy IP and `TRUSTED_PROXIES` together; treating only one side as changed is a common source of broken real-client IP handling.
- Rotating the application secret:
  treat it as planned maintenance; active sessions and sealed cookies will stop working, which is expected — and that is only the visible tip. The full per-domain impact (2FA secrets, stored webhook URLs, calendar feeds of every generation) with recovery steps lives in [Secret Handling and Rotation](#secret-handling-and-rotation).
- Seeing healthy containers but a failing public URL:
  check DNS, certificate mounts, and proxy config before changing application data or restoring backups.

## Advanced Deployment Path

The advanced path is still self-hosted, single-instance Ovumcy. It is for operators who want stronger operational discipline without changing the product model, introducing multi-tenant hosting, or moving beyond the SQLite baseline without inventing unsupported storage behavior.

Use it only after the baseline path is already stable.

Recommended advanced practices:

- Build on the baseline backup contract above with an off-host copy and periodic restore drills into an isolated temporary stack; see [Backup and Restore Contract](#backup-and-restore-contract) for the restore/verification steps. Drill into a **populated** stack, not a fresh empty one. Restoring into an empty database is the one case where a broken restore procedure still looks correct, so a drill that starts from nothing passes whether or not the procedure works. Most real recoveries are not that case: partial loss, a bad import, a rollback to yesterday's state all restore over a database that still holds data. Restore the backup, use the instance until its contents have visibly drifted from the dump, then restore the same backup over it and finish with [Post-Restore Verification](#post-restore-verification), step 5 included.
- Restrict Docker, shell, and filesystem access so that only a small number of administrators can read `.env`, logs, the SQLite volume, or backup archives.
- Rotate or ship logs to a private operator-controlled sink, and keep retention short enough that routine diagnostics do not become a second long-term data store.
- Monitor host disk space, backup-job success, container health, and the last known-good image tag so upgrades and restores remain predictable.
- Keep public exposure narrow: only the reverse proxy should publish host ports, and firewall rules should match that design instead of relying on the app container to be unreachable by accident.

Optional Postgres is part of this advanced path, not the baseline:

- Set `DB_DRIVER=postgres` and provide `DATABASE_URL`.
- Keep SQLite as the default unless you actively want an operator-managed database service.
- The repository's SQLite backup/restore runbook does not apply to Postgres; use native Postgres backup tooling and restore drills instead, including for the bundled local/private Postgres stack.
- Use the bundled local/private Postgres stack under `docs/examples/postgres/` when you want an official advanced deployment path without designing your own database compose topology first.
- Use the dedicated Postgres reverse-proxy stacks under `docs/examples/reverse-proxy/*-postgres/` when you need the preferred public self-hosted exposure model with Postgres.
- Existing SQLite deployments are not auto-migrated. A PostgreSQL deployment is a separate runtime choice unless and until a dedicated migration tool is introduced.

This guide does not define an advanced managed platform. It still assumes one private deployment, operator-managed infrastructure, and the existing SQLite application contract.
