### Fixed

- **Operator documentation matches the shipped binary.** The README and architecture overview now list every operator subcommand (`link-oidc-identity`, `healthcheck`, `readycheck` were missing), the GDPR guide describes email rectification through `ovumcy users set-email` and names every field encrypted at rest, the cookie table lists the OIDC step-up continuation and identity-link confirmation cookies, and the Postgres and reverse-proxy-Postgres example `.env.example` files carry every variable their compose files pass through.
