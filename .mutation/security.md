# Mutation baseline — `internal/security`

Authoritative gremlins run for the auth/crypto/OIDC package (reproduce with
`scripts/mutation.sh baseline`); this file is the reviewed summary.

## Score (measured on `54a29ad`)

| Metric | Value |
|--------|-------|
| Killed | 99 |
| Lived (survivors) | 9 — documented below (equivalent or e2e/host-covered) |
| Timed out | 6 |
| Not covered | 17 |
| **Test efficacy** | **91.67%** — killed / (killed + lived) = 99 / 108 |
| Mutator coverage | 86.40% |

The OIDC client's provider-facing code (discovery, code exchange, token
verification, TLS bootstrap) is exercised by the end-to-end OIDC lanes
(Playwright, three browsers), not by Go unit tests — mutation runs only
`go test`, so those lines survive or show "not covered" here while being covered
at the e2e layer.

## Documented survivors

### Equivalent mutants

**`sealed_cipher.go:115` — AEAD payload-length guard (BOUNDARY).**
`len(payload) <= nonceSize` → `< nonceSize`. At `len == nonceSize` the mutant
proceeds with an empty ciphertext; AES-256-GCM `Open` then fails authentication
(ciphertext shorter than the 16-byte tag) and returns an error with nil
plaintext — the same observable outcome as the explicit "payload too short"
rejection. No plaintext is produced; only the error text differs.

**`oidc.go:260:51` — trailing-`@` guard in AllowsAutoProvision (ARITHMETIC, INVERT).**
`atIndex == len(email)-1` rejects an address ending in `@`. Removing/inverting it
lets the code proceed, but it extracts an empty domain that matches no allowed
domain and returns `false` anyway. (The `atIndex < 0` half is a real boundary,
killed by a test.)

**`oidc.go:578:9` — CA-bundle read-error check (NEGATION).**
On a failed read the content is nil; the mutant skips the early `return err`,
then `AppendCertsFromPEM(nil)` is false and it rejects with "must contain at
least one PEM certificate". Both paths reject the unreadable bundle; only the
error text differs.

**`oidc.go:594:15` — defensive transport nil-check (NEGATION).**
`http.DefaultTransport` is always a non-nil `*http.Transport`, so the guarded
branch is unreachable in practice; both branches yield a functional client.

### Covered by the e2e OIDC lanes / host environment

**`oidc.go:390:24`, `oidc.go:390:50` — provider cache fast-path (NEGATION).**
Reaching the cached branch needs a previously discovered provider (live issuer +
JWKS). The e2e lanes drive repeated authorize/exchange calls that reuse it; a Go
unit cannot build a real verifier without a provider.

**`oidc.go:603:15`, `oidc.go:603:31` — system cert-pool fallback (NEGATION).**
The `SystemCertPool` failure branch cannot be forced from a test on a normally
configured host; the success path is taken and the mutants do not change the
resulting (functional) client observably.

## Not covered

The 17 not-covered mutants are the provider-facing OIDC methods `AuthCodeURL`
and `ExchangeCode` (lines 295–356), which need a live provider and are driven by
the e2e OIDC lanes, plus the package-level `const defaultOIDCHTTPTimeout =
10 * time.Second`, which Go statement coverage never instruments.
