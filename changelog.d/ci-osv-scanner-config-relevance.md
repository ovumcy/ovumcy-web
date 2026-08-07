### Internal

- **The repo-root `osv-scanner.toml` is documented as live configuration, with its consumer named.**
  It was unreferenced anywhere in the tree, so its relevance kept being re-opened. The OpenSSF
  Scorecard workflow's Vulnerabilities check embeds the osv-scanner library and scans the
  checked-out tree; with no config path passed, osv-scanner resolves its config next to the scanned
  manifest, which for the repo-root `go.mod` is that file. Verified by running the same
  osv-scanner version against `go.mod`: it loads the file and filters `GO-2026-5932`, the advisory
  that covers every `golang.org/x/crypto` version with no fixed release. The file now says which
  workflow consumes it, and `docs/SECURITY_INVARIANTS.md → CI` records the suppression alongside
  the digest-pinning exception.
