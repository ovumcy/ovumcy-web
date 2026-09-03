### Added

- **`DATABASE_URL_FILE` and `OIDC_CLIENT_SECRET_FILE` join `SECRET_KEY_FILE` as Docker Swarm/Compose
  secrets routes.** The runtime image ships shell-free, so the usual `sh -c 'export
  X=$(cat …)'` workaround to read a mounted secret file has no shell to run it in — `SECRET_KEY_FILE`
  was already the sanctioned way around that for the application secret, and the Postgres DSN and the
  OIDC client secret are the two other operator-supplied values that carry a live credential. Both
  read through the same bounded local-file helper as `SECRET_KEY_FILE` (rejects directories and
  special files, caps the read, trims a trailing newline) and the same precedence: the plain
  variable wins when both are set. Unlike a wrong `SECRET_KEY`, which fails loudly at first use, a
  wrong `DATABASE_URL` just points at the wrong database with no error — so that precedence is no
  longer silent: boot now logs one line per resolved value naming which variable supplied it, and
  names the `_FILE` variable specifically when it was set but ignored (never the value itself). A
  sqlite instance carrying a stale or dangling `DATABASE_URL_FILE` it never reads boots as before —
  the file is only opened when `DB_DRIVER=postgres` actually consumes it. The bundled and example
  compose stacks and `.env.example` now carry both variables alongside their existing `_FILE`
  sibling.
