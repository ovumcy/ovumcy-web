### Dependencies

- **Runtime Go modules** moved since v1.9.2: `gofiber/fiber/v3` 3.4.0 to 3.5.0, `jackc/pgx/v5`
  5.10.0 to 5.11.0, `coreos/go-oidc/v3` 3.20.0 to 3.21.0, `golang.org/x/crypto` 0.54.0 to 0.57.0,
  `golang.org/x/net` 0.57.0 to 0.59.0, `golang.org/x/oauth2` 0.36.0 to 0.37.0, `golang.org/x/sys`
  0.47.0 to 0.48.0 and `gorm.io/driver/postgres` 1.6.0 to 1.6.3, with their indirect dependencies.
  The Go toolchain itself is 1.27.1 (see the Security entry), and the runtime image's Alpine base
  moves from 3.24.1 to 3.24.2.
- **npm toolchain** (development only, nothing in the shipped image): `@playwright/test` 1.61.1 to
  1.63.0, `eslint` 10.7.0 to 10.11.0, `globals` 17.7.0 to 17.12.0, `jsdom` 29.1.1 to 30.1.1,
  `otplib` 13.4.1 to 13.5.0, `htmx.org` 2.0.10 to 2.0.11 (the committed bundle is regenerated), and
  the `brace-expansion` override raised to 5.0.11 past two advisories.
- **GitHub Actions** pins: `actions/attest-build-provenance` 4.1.1 to 4.2.2, `actions/checkout`
  7.0.0 to 7.0.1, `codecov/codecov-action` 7.0.0 to 7.1.1, `docker/build-push-action` 7.3.0 to 7.4.0,
  `docker/login-action` 4.4.0 to 4.6.0, `docker/setup-buildx-action` 4.2.0 to 4.4.1,
  `docker/setup-qemu-action` 4.2.0 to 4.4.0, `github/codeql-action` 4.37.1 to 4.38.2 and
  `ossf/scorecard-action` 2.4.3 to 2.4.4.

### Changed

- **The `osv-scanner.toml` suppression of GO-2026-5932 now expires.** It carries an `ignoreUntil`
  date, so the unreachable `golang.org/x/crypto/openpgp` finding is re-judged instead of staying
  silenced indefinitely.
