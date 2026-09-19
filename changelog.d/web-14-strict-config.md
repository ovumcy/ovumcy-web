### Changed

- **An unparseable `AUDIT_LOG_ENABLED` or `OIDC_ENABLED` now refuses the boot.** Both join
  `COOKIE_SECURE`, `HSTS_ENABLED`, `TRUST_PROXY_ENABLED` and `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`: a
  typo such as `AUDIT_LOG_ENABLED=ture` used to start the instance with the audit stream off, and
  `OIDC_ENABLED=ture` with OIDC off — which also skipped every OIDC check and, under
  `OIDC_LOGIN_MODE=oidc_only`, quietly brought password sign-in back — each with only one warning
  in the boot log. Both now exit with an error naming the key and the value. The accepted spellings
  are unchanged (`1`/`true`/`yes`/`on`, `0`/`false`/`no`/`off`), and an unset key still means off.
  **Upgrade note:** a value that only produced that warning before now stops the start — check the
  boot log of the running version for `invalid AUDIT_LOG_ENABLED` or `invalid OIDC_ENABLED` before
  upgrading. A common source is a quoted value passed through `docker run --env-file`, which keeps
  the quotes (`"false"`).

### Fixed

- **`OIDC_CLIENT_SECRET_FILE` no longer blocks the boot of an instance with OIDC disabled.** The
  client secret was resolved before `OIDC_ENABLED` was consulted, so a stale or unreadable secret
  file path refused the start of an instance that never uses it. With `OIDC_ENABLED` off neither
  `OIDC_CLIENT_SECRET` nor the file is read. With OIDC enabled nothing changes:
  `OIDC_CLIENT_SECRET` still wins when both are set, and an unreadable file that is the only source
  still refuses the boot.
