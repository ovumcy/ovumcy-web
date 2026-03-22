# Self-Hosted Operations Guide

Ovumcy's supported self-hosted baseline is a single application instance with a persistent SQLite volume, HTTPS at the edge, and a strong application secret. The goal of this guide is not to describe every possible deployment, but to define a production-safe path that ordinary self-hosters can follow without inventing their own operational rules.

## Baseline Contract

Supported baseline assumptions:

- One Ovumcy instance per private deployment.
- Persistent storage for `/app/data`.
- HTTPS termination at a trusted reverse proxy or load balancer.
- `COOKIE_SECURE=true` when traffic is served over HTTPS.
- `TRUST_PROXY_ENABLED=true` only when Ovumcy is actually behind your own trusted reverse proxy.
- Prefer a containerized reverse proxy stack where only the proxy publishes host ports.
- Keep Ovumcy's plain HTTP port internal to a private network or loopback-only.
- A strong, unique application secret provided through `SECRET_KEY` or `SECRET_KEY_FILE`.

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
- `PROXY_HEADER=X-Forwarded-For`
- `TRUSTED_PROXIES` must match the exact proxy IP or private Docker subnet used by that stack
- when `COOKIE_SECURE=true`, Ovumcy emits `Strict-Transport-Security` itself, so the example proxy configs do not add a second HSTS policy

Do not start from the base compose file and then expose `8080` publicly as a shortcut. The supported public path is the dedicated proxy stack where only the reverse proxy publishes host ports.

### Advanced knobs

These settings are valid, but they are not required for a safe first deployment:

- `TZ` and `DEFAULT_LANGUAGE` (`en`, `ru`, `es`, `fr`, `de`) for operator preference
- rate-limit variables if you need stricter or looser local policy
- `PROXY_HEADER` only if your trusted proxy uses a different real-client header contract
- `DB_DRIVER=postgres` plus `DATABASE_URL=...` when you intentionally move the app runtime to Postgres, either through the bundled local/private Postgres stack or an operator-managed database service

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
- Keep `PROXY_HEADER=X-Forwarded-For` unless you have a concrete reason to change it.

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

These stacks keep the public contract tight:

- only the proxy publishes `80/443`;
- `ovumcy` and `postgres` stay on the internal Docker network;
- `DB_DRIVER=postgres` and `DATABASE_URL` are already wired;
- the proxy subnet is already aligned with `TRUSTED_PROXIES`.

Use them when you need both:

- advanced self-hosted Postgres as the runtime engine;
- the preferred public self-hosted proxy-only exposure model.

## Health Checks by Deployment Mode

Use the health check that matches your deployment path:

- Public reverse-proxy stack:
  - `docker compose ps` should show `ovumcy` as healthy;
  - `curl -fsS https://your-domain.example/healthz` should succeed through the proxy.
- Local/private base compose path:
  - `docker compose ps` should show the container healthy;
  - `curl -fsS http://127.0.0.1:8080/healthz` should succeed on the host.

For the public reverse-proxy stacks, do not treat a missing host-level `127.0.0.1:8080` listener as a problem. In the preferred deployment model, that port is intentionally not published to the host at all.

## Secret Handling and Rotation

Treat the application secret as part of the deployment identity, whether you pass it via `SECRET_KEY` or `SECRET_KEY_FILE`.

- `SECRET_KEY_FILE` should point to a readable path inside the running process or container. Trailing newlines are trimmed, but the secret still needs 32+ non-placeholder characters.
- `SECRET_KEY` takes precedence if both secret sources are configured.
- Store the underlying secret privately and back it up separately from the SQLite archive.
- Rotating the application secret invalidates existing sealed cookies and active sign-ins.
- Restoring SQLite data with a different application secret is valid, but users should expect a fresh sign-in and new sealed-cookie state.
- Do not paste the application secret, backup archives, or certificate material into issue trackers, chat logs, or shared shell history.

## Backup and Restore Contract

The supported self-hosted backup contract is intentionally narrow:

- Back up the SQLite data volume before every upgrade and before any manual recovery work.
- Treat every backup archive as sensitive health data.
- Keep `.env` and the application secret backup (`SECRET_KEY` value or the file behind `SECRET_KEY_FILE`) separate from the SQLite data archive.
- Expect existing auth-related cookies to become invalid if you restore data with a different application secret.

For the bundled local/private Postgres stack, use native PostgreSQL backup tooling instead of the SQLite archive workflow:

```bash
mkdir -p backups
docker compose exec -T postgres sh -lc 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > backups/ovumcy-postgres.sql
```

Restore with the app stopped and the target database intentionally selected:

```bash
cat backups/ovumcy-postgres.sql | docker compose exec -T postgres sh -lc 'psql -U "$POSTGRES_USER" "$POSTGRES_DB"'
```

Keep the SQL dump and `.env` / application-secret backup separate, just as you would for the SQLite baseline.

The same rule applies to the public Postgres reverse-proxy stacks. Back up PostgreSQL with `pg_dump` or your platform-native Postgres snapshot tooling; do not try to apply the SQLite file-copy runbook to those stacks.

Recommended baseline:

- Use the default Docker named volume when possible.
- Keep at least one recent rollback backup before replacing production data.
- Verify a restore with `/healthz` and a normal page load before trusting it.

Bind mounts are still valid, but they are an advanced operator path. For bind mounts, stop the app and back up the mounted directory with normal filesystem tools while preserving file contents and access permissions.

## Docker Named Volume Backup

The default compose deployment uses the `ovumcy_data` named volume. A portable manual backup flow is:

```bash
mkdir -p backups
BACKUP_FILE="ovumcy-data-backup.tgz"

docker run --rm \
  -e BACKUP_FILE="$BACKUP_FILE" \
  -v ovumcy_data:/source:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.22.3 \
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
  alpine:3.22.3 \
  sh -c 'cd /target && tar xzf "/backup/$BACKUP_FILE"'

docker compose up -d
```

Before removing the existing volume, make a fresh rollback backup if you are not already holding one you trust.
When you restore into a manually recreated named volume, Docker Compose may print a warning that the volume was not created by Compose. In this workflow that warning is expected and does not by itself mean the restore failed.
After startup, verify the restored app using the health check appropriate for your deployment mode.

## Post-Restore Verification

After restore:

1. Confirm the container becomes healthy.
2. Confirm `/healthz` responds successfully using the health check appropriate for your deployment mode.
3. Open the main UI once and verify the app renders normally.
4. If you restored with a different `SECRET_KEY`, expect existing auth sessions and sealed cookies to be invalid and require a fresh sign-in.

## Safe Upgrade Procedure

Use this sequence for routine upgrades:

1. Confirm you know where the persistent volume or bind mount is stored.
2. Take a backup of the database before changing the image version.
3. Pull the new image and restart the service.
4. Wait for the container healthcheck to report healthy.
5. Confirm `/healthz` through the correct deployment-mode health check and open the main UI once to confirm the app is responding.
6. If the new version fails to start cleanly, roll back to the previous image tag and restore from backup if needed.

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
- Config issue: container runs but startup logs show invalid env values or trusted-proxy configuration errors.
- Proxy issue: `ovumcy` is healthy, but public requests fail, loop, or lose the real client IP.

## Common Operator Scenarios

- Moving from local/private to public HTTPS:
  start from the dedicated Caddy or Nginx example stack, then migrate your existing SQLite volume into that stack instead of exposing the base compose app port directly.
- Changing the proxy subnet or host:
  update the Docker subnet or proxy IP and `TRUSTED_PROXIES` together; treating only one side as changed is a common source of broken real-client IP handling.
- Rotating the application secret:
  treat it as planned maintenance; active sessions and sealed cookies will stop working, which is expected.
- Seeing healthy containers but a failing public URL:
  check DNS, certificate mounts, and proxy config before changing application data or restoring backups.

## Advanced Deployment Path

The advanced path is still self-hosted, single-instance Ovumcy. It is for operators who want stronger operational discipline without changing the product model, introducing multi-tenant hosting, or moving beyond the SQLite baseline without inventing unsupported storage behavior.

Use it only after the baseline path is already stable.

Recommended advanced practices:

- Keep at least one recent off-host backup copy of the SQLite archive and store the `.env` / application-secret backup separately from that data copy.
- Run periodic restore drills into an isolated temporary stack and verify both `/healthz` and a normal page load before trusting the backup chain.
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
