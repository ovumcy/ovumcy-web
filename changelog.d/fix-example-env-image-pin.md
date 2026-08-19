### Fixed

- **The two public Postgres example env templates no longer downgrade the stack they ship with.**
  `docs/examples/reverse-proxy/caddy-postgres/.env.example` and its nginx twin pinned
  `OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web:v0.8.4`, while the `docker-compose.yml` beside each one
  defaults to `${OVUMCY_IMAGE:-ghcr.io/ovumcy/ovumcy-web:v1.9.2}`. The runbook links both files as
  the env template and instructs an operator to copy them to `.env` before the first start, and an
  assignment in `.env` wins over the compose default — so the documented setup path deployed a
  pre-1.0 image on a stack written for the current release, skipping every intervening fix and
  migration. Both now pin `v1.9.2`; the three other examples carry no override and already floated
  to the compose default.

### Internal

- **`scripts/readmeversion` now also guards the example env templates against release-tag drift.**
  Nothing re-checked those pins when the release tag moved, which is why they were six minors
  behind. `TestExampleEnvImagePinsMatchReleaseTag` walks every `.env.example` under
  `docs/examples/`, and requires an active `OVUMCY_IMAGE` assignment to name the published image at
  the exact release tag README.md asserts. It fails closed the way the neighbouring guards do: no
  templates, no pin in any of them, or a pin that is not an exact release tag (`:latest`, a digest)
  each fail rather than pass quietly.
