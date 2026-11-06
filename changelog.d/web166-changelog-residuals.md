### Changed

- **The `osv-scanner.toml` suppression of GO-2026-5932 now expires.** It carries an `ignoreUntil`
  date, so the unreachable `golang.org/x/crypto/openpgp` finding is re-judged instead of staying
  silenced indefinitely.
