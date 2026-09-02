### Fixed

- **`docker-compose.yml` no longer lets `PORT` and `TZ` silently diverge from `.env`.** The bundled
  compose file read `${PORT:-8080}` for the `ports:` host **and** container mapping, but the
  container's actual listening port came only from `env_file: ./.env` — `PORT=9090 docker compose
  up` published `9090:9090` while the process kept listening on `.env`'s `8080`, making the service
  unreachable with a green internal healthcheck. Separately, `TZ=${TZ:-UTC}` sat inside
  `environment:`, which always outranks `env_file`, so a leftover shell `TZ` silently beat `.env`'s
  `TZ` — and timezone decides cycle-day boundaries. `PORT` now lives in `environment:` so the ports
  mapping and the container's env come from the same substitution; `TZ` now comes only from
  `env_file`.
