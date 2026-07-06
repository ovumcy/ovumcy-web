# Mutation baseline — `internal/security`

Authoritative gremlins run for the auth/crypto/OIDC package (reproduce with
`scripts/mutation.sh baseline`); this file is the reviewed summary.

## Score (measured on `a6d7e41`)

**Source: workflow_dispatch run
[28758365493](https://github.com/ovumcy/ovumcy-web/actions/runs/28758365493)**
on `mutation.yml` (`mutation-baseline (internal_security)` job, single
unsharded run, green) — the same run that refreshed `internal-api.md` (PR
#174) and `internal-services.md`. Numbers confirmed two independent ways:
(a) gremlins' own end-of-run summary line in the job log, (b) counting
`"status"` values directly in the downloaded `internal_security.json`. Both
agree exactly.

| Metric | Value |
|--------|-------|
| Killed | 112 |
| Lived (survivors) | 9 — documented below (equivalent or e2e/host-covered) |
| Timed out | 0 |
| Not covered | 17 |
| **Test efficacy** | **92.56%** — killed / (killed + lived) = 112 / 121 |
| Mutator coverage | 87.68% — (killed + lived) / (killed + lived + not covered) = 121 / 138 |

The OIDC client's provider-facing code (discovery, code exchange, token
verification, TLS bootstrap) is exercised by the end-to-end OIDC lanes
(Playwright, three browsers), not by Go unit tests — mutation runs only
`go test`, so those lines survive or show "not covered" here while being covered
at the e2e layer.

## Documented survivors

Reconciled against the `28758365493` survivor set: all 9 mutants below are
still present and still carry the same argument (equivalent, or covered by the
e2e OIDC lanes) — only line numbers shifted, since commits since `54a29ad`
(security hardening, robustness/auth follow-up, the v1.6.0 release) added code
earlier in `oidc.go`. No previously-documented survivor was killed, and no new
survivor appeared — this is a like-for-like refresh, not a re-triage.

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

**`oidc.go:613:9` — CA-bundle read-error check (NEGATION).**
On a failed read the content is nil; the mutant skips the early `return err`,
then `AppendCertsFromPEM(nil)` is false and it rejects with "must contain at
least one PEM certificate". Both paths reject the unreadable bundle; only the
error text differs.

**`oidc.go:629:15` — defensive transport nil-check (NEGATION).**
`http.DefaultTransport` is always a non-nil `*http.Transport`, so the guarded
branch is unreachable in practice; both branches yield a functional client.

### Covered by the e2e OIDC lanes / host environment

**`oidc.go:390:24`, `oidc.go:390:50` — provider cache fast-path (NEGATION).**
Reaching the cached branch needs a previously discovered provider (live issuer +
JWKS). The e2e lanes drive repeated authorize/exchange calls that reuse it; a Go
unit cannot build a real verifier without a provider.

**`oidc.go:638:15`, `oidc.go:638:31` — system cert-pool fallback (NEGATION).**
The `SystemCertPool` failure branch cannot be forced from a test on a normally
configured host; the success path is taken and the mutants do not change the
resulting (functional) client observably.

## Not covered

12 of the 17 not-covered mutants are the provider-facing OIDC methods
`AuthCodeURL` (line 290) and `ExchangeCode` (line 313), spanning lines
295–356, which need a live provider and are driven by the e2e OIDC lanes. 1 is
the package-level `const defaultOIDCHTTPTimeout = 10 * time.Second` (line 21),
which Go statement coverage never instruments — both as previously documented,
just at a shifted line range.

The remaining 4 are new since the last measurement: `OIDCConfig.validateRequiredFields`'s
switch-case branches (`oidc.go:144,146,148,150`) that reject a missing
`OIDC_ISSUER_URL`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET`/`OIDC_REDIRECT_URL`.
This validation runs once at bootstrap with a fixed, operator-supplied config —
not exercised by a `go test` unit today. Whether that's an acceptable gap (config
validation is arguably an integration/bootstrap concern) or worth a small
table-driven unit test is a call for the next triage pass, not asserted here.
