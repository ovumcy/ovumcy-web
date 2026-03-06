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
- A strong, unique `SECRET_KEY`.

Out of scope for this baseline:

- Hosted multi-tenant deployments.
- Shared databases across multiple independent users or organizations.
- Backup automation and disaster recovery orchestration beyond the manual operator workflow described here.

## Production Checklist

Before exposing Ovumcy outside localhost:

1. Generate a strong `SECRET_KEY` and store it privately.
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

- `SECRET_KEY` must be strong, private, and backed up separately from SQLite data.
- Persistent storage must exist for `/app/data`.
- You must know whether you are running the local/private base compose path or a public reverse-proxy stack before changing cookie and proxy settings.

### Local/private base compose path

Use the repository root `docker-compose.yml` for localhost, LAN, or other private-network deployments:

- `COOKIE_SECURE=false` unless you terminate HTTPS before the app.
- `TRUST_PROXY_ENABLED=false` unless you have explicitly placed Ovumcy behind your own trusted proxy.
- `PORT=8080` is expected to be reachable only on the host or private network you control.

### Public reverse-proxy stack

Use one of the example stacks under `docs/examples/reverse-proxy/` for public HTTPS deployments:

- `COOKIE_SECURE=true`
- `TRUST_PROXY_ENABLED=true`
- `PROXY_HEADER=X-Forwarded-For`
- `TRUSTED_PROXIES` must match the exact proxy IP or private Docker subnet used by that stack

Do not start from the base compose file and then expose `8080` publicly as a shortcut. The supported public path is the dedicated proxy stack where only the reverse proxy publishes host ports.

### Advanced knobs

These settings are valid, but they are not required for a safe first deployment:

- `TZ` and `DEFAULT_LANGUAGE` for operator preference
- rate-limit variables if you need stricter or looser local policy
- `PROXY_HEADER` only if your trusted proxy uses a different real-client header contract

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

- Caddy:
  - Compose stack: [docs/examples/reverse-proxy/caddy/docker-compose.yml](examples/reverse-proxy/caddy/docker-compose.yml)
  - Proxy config: [docs/examples/reverse-proxy/caddy/Caddyfile](examples/reverse-proxy/caddy/Caddyfile)
- Nginx:
  - Compose stack: [docs/examples/reverse-proxy/nginx/docker-compose.yml](examples/reverse-proxy/nginx/docker-compose.yml)
  - Proxy config: [docs/examples/reverse-proxy/nginx/nginx.conf](examples/reverse-proxy/nginx/nginx.conf)

Both examples assume:

- the public hostname is `ovumcy.example.com`;
- you create a local `.env` file next to the example `docker-compose.yml` with at least `SECRET_KEY=...`;
- the `ovumcy` service stays on a private Docker network and is not reachable directly from the host network;
- public traffic reaches only the reverse proxy.

Prefer the Caddy stack if you want automatic certificate management. Use the Nginx stack if you already manage TLS certificates yourself and can mount them into `./certs/fullchain.pem` and `./certs/privkey.pem`.

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

Treat `SECRET_KEY` as part of the deployment identity, not as an ordinary tuning variable.

- Store it privately and back it up separately from the SQLite archive.
- Rotating `SECRET_KEY` invalidates existing sealed cookies and active sign-ins.
- Restoring SQLite data with a different `SECRET_KEY` is valid, but users should expect a fresh sign-in and new sealed-cookie state.
- Do not paste `SECRET_KEY`, backup archives, or certificate material into issue trackers, chat logs, or shared shell history.

## Backup and Restore Contract

The supported self-hosted backup contract is intentionally narrow:

- Back up the SQLite data volume before every upgrade and before any manual recovery work.
- Treat every backup archive as sensitive health data.
- Keep `.env` and `SECRET_KEY` backed up separately from the SQLite data archive.
- Expect existing auth-related cookies to become invalid if you restore data with a different `SECRET_KEY`.

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
  alpine:3.21 \
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
  alpine:3.21 \
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

For the public reverse-proxy example stacks, run the same `docker compose pull`, `docker compose up -d`, and `docker compose ps` sequence inside the example directory, then verify `https://your-domain.example/healthz` through the proxy instead of expecting a host-level `127.0.0.1:8080` listener.

For safer upgrades, pin `OVUMCY_IMAGE` to a concrete release tag instead of relying on `latest`.

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

Typical failure split:

- App issue: container exits, the container healthcheck fails, or `/healthz` fails inside the intended deployment path.
- Config issue: container runs but startup logs show invalid env values or trusted-proxy configuration errors.
- Proxy issue: `ovumcy` is healthy, but public requests fail, loop, or lose the real client IP.

## Common Operator Scenarios

- Moving from local/private to public HTTPS:
  start from the dedicated Caddy or Nginx example stack, then migrate your existing SQLite volume into that stack instead of exposing the base compose app port directly.
- Changing the proxy subnet or host:
  update the Docker subnet or proxy IP and `TRUSTED_PROXIES` together; treating only one side as changed is a common source of broken real-client IP handling.
- Rotating `SECRET_KEY`:
  treat it as planned maintenance; active sessions and sealed cookies will stop working, which is expected.
- Seeing healthy containers but a failing public URL:
  check DNS, certificate mounts, and proxy config before changing application data or restoring backups.
