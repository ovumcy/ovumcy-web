### Fixed

- **The deployment documentation's commands and examples now work on the published example stacks.**
  The operator CLI and probe commands use `docker compose exec ovumcy …` (`-T` for a piped
  password) instead of `docker exec ovumcy …`, because only the root compose file names its
  container `ovumcy`. Both nginx example configs set `client_max_body_size 16m`, so a JSON restore
  within the app's 16 MiB limit is no longer refused by nginx's 1 MB default. The self-hosting guide
  now states that a bind-mounted data directory must be owned by UID/GID 10001, and that
  `POSTGRES_PASSWORD` must be URL-safe because it is placed unescaped into `DATABASE_URL`.
  No runtime behavior change.
