### Fixed

- **A mistyped security setting now refuses the boot instead of running on the default.** A value the
  app cannot parse for `COOKIE_SECURE`, `HSTS_ENABLED`, `TRUST_PROXY_ENABLED` or
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` (`COOKIE_SECURE=ture`) used to log a line and start on the
  fallback, so what an instance actually ran with could not be read from the env file. With
  `TRUST_PROXY_ENABLED=true`, a `TRUSTED_PROXIES` entry the trust boundary cannot use — a broken CIDR,
  a non-IP string, or an IP not written in canonical form — was likewise dropped, leaving the proxy
  untrusted so every client behind it shared one rate-limit bucket while the startup banner counted
  the raw list and reported the typo as configured. Both now stop the process with an error naming
  the key and the rejected value. Unset keys still mean their documented defaults, and every other
  env var keeps its fallback behaviour.
