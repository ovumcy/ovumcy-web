# Self-Hosted Operations Guide

Ovumcy's supported self-hosted baseline is a single application instance with a persistent SQLite volume, HTTPS at the edge, and a strong application secret. The goal of this guide is not to describe every possible deployment, but to define a production-safe path that ordinary self-hosters can follow without inventing their own operational rules.

## Baseline Contract

Supported baseline assumptions:

- One Ovumcy instance per private deployment.
- Persistent storage for `/app/data`.
- HTTPS termination at a trusted reverse proxy or load balancer.
- `COOKIE_SECURE=true` when traffic is served over HTTPS.
- `TRUST_PROXY_ENABLED=true` only when Ovumcy is actually behind your own trusted reverse proxy.
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
6. Restrict who can access container logs, `.env`, backups, and the SQLite data volume.
7. Verify the container becomes healthy before relying on the deployment.

## Safe Upgrade Procedure

Use this sequence for routine upgrades:

1. Confirm you know where the persistent volume or bind mount is stored.
2. Take a backup of the database before changing the image version.
3. Pull the new image and restart the service.
4. Wait for the container healthcheck to report healthy.
5. Open `/healthz` and the main UI once to confirm the app is responding.
6. If the new version fails to start cleanly, roll back to the previous image tag and restore from backup if needed.

Practical Docker flow:

```bash
docker compose pull
docker compose up -d
docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
```

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

3. Check the local health endpoint:

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

4. If the app is healthy locally but public access fails, inspect the reverse proxy or TLS configuration first.
5. If the app is not healthy locally, inspect environment variables, permissions on the persistent volume, and the current image tag before changing application data.

Typical failure split:

- App issue: container exits, `/healthz` fails locally, startup logs show application errors.
- Config issue: container runs but startup logs show invalid env values or trusted-proxy configuration errors.
- Proxy issue: container is healthy locally but public requests fail, loop, or lose the real client IP.
