### Added

- **`DATABASE_URL_FILE` and `OIDC_CLIENT_SECRET_FILE` join `SECRET_KEY_FILE` as Docker Swarm/Compose
  secrets routes.** The runtime image ships shell-free, so the usual `sh -c 'export
  X=$(cat …)'` workaround to read a mounted secret file has no shell to run it in — `SECRET_KEY_FILE`
  was already the sanctioned way around that for the application secret, and the Postgres DSN and the
  OIDC client secret are the two other operator-supplied values that carry a live credential. Both
  read through the same bounded local-file helper as `SECRET_KEY_FILE` (rejects directories and
  special files, caps the read, trims a trailing newline) and the same precedence: the plain
  variable wins silently when both are set. The bundled and example compose stacks and `.env.example`
  now carry both variables alongside their existing `_FILE` sibling.
