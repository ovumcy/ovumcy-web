### Dependencies

- **`github.com/go-jose/go-jose/v4` 4.1.4 → 4.1.6** (indirect, through `go-oidc`): fixes
  CVE-2026-116587, a denial of service when parsing a JWK set that contains RSA keys. The JWK set
  comes from the operator-configured OIDC provider.
