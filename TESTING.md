# Testing & Quality

Ovumcy stores sensitive reproductive-health data, so correctness and privacy are
treated as features, not afterthoughts. This document describes how the project
is tested — and, just as importantly, how we verify that the tests themselves are
worth anything. Every claim here is backed by code in the repository and by CI.

## Layers

| Layer | What it checks | Where |
|-------|----------------|-------|
| **Unit** | Business/domain logic (cycle math, validation, policies) | `internal/services/*_test.go` |
| **Integration** | HTTP handlers against a real database, persistence correctness | `internal/api/*_test.go` |
| **End-to-end** | Real user flows in a browser, across Chromium / Firefox / WebKit | `e2e/*.spec.ts` (Playwright) |
| **Property-based** | Invariants of the cycle math over thousands of generated inputs | `internal/services/cycles_property_test.go` (`pgregory.net/rapid`) |
| **Fuzz** | Robustness of parsers/validators against arbitrary/invalid input | `internal/services/policy_fuzz_test.go` (native Go fuzzing) |
| **Reference vectors** | Cycle predictions match the documented algorithm, number for number | `internal/services/cycles_reference_test.go` |

Currently **1,375+ Go test functions** across `internal/` and **25 Playwright
specs** (run on three browser engines). Tests favor behavior and persisted state
over markup or implementation details.

## We test our tests

High coverage proves code *ran*, not that a test would *fail if the code broke*.
We close that gap with **mutation testing** ([gremlins](https://github.com/go-gremlins/gremlins)):
it injects faults into the production code and checks that at least one test
fails ("kills" the mutant). Surviving mutants reveal weak assertions.

- Run it locally: `scripts/mutation.sh baseline` (full) or `scripts/mutation.sh diff <ref>` (changed code only).
- A weekly CI job tracks the trend; it is advisory and never blocks a merge.
- **Mutation efficacy** (gremlins, killed / (killed + survived); tracked weekly): `internal/services` **99.7%** (1462/1466) and `internal/security` **91.7%** (99/108). Every surviving mutant is a documented equivalent (an unreachable guard, a redundant clamp, an error-text-only difference) or an OIDC provider path covered by the e2e lanes rather than Go units — classified, not chased. Per-package breakdowns live in [`.mutation/`](.mutation/).
- Statement coverage is lower than efficacy by design: mutation testing checks whether a test *fails when the code breaks*, not merely whether a line ran. The "not covered" mutants are dominated by package-level `const`/`var` declarations (which Go coverage never instruments) and the network-facing OIDC client (covered end-to-end).

Surviving mutants are triaged honestly: a *real* gap gets a new behavior test; an
*equivalent* mutant (one that cannot change any observable outcome — a log line,
an error string, an unreachable guard) is documented rather than papered over with
a brittle test. We do not chase a fake 100%.

## Security & supply chain

| Tool | Purpose |
|------|---------|
| `staticcheck` + `go vet` | Static analysis |
| [`gosec`](https://github.com/securego/gosec) | Go security (SAST), results in the GitHub Security tab |
| [`govulncheck`](https://golang.org/x/vuln) | Call-graph reachability gate — fails CI only on vulnerabilities the code actually reaches |
| [Trivy](https://trivy.dev) | Dependency and container image scanning (fails on fixed HIGH/CRITICAL) |
| CycloneDX SBOM | Software bill of materials generated for the runtime image |

The runtime image is a `FROM scratch` multi-stage build running as a non-root
user, with pinned base-image digests and dependency versions. Test code never
ships in the image.

### Sealed-cookie codec coverage

All eleven AEAD-sealed cookie purposes are exercised by `internal/api/secure_cookie_codec_security_test.go`.
Each purpose is bound to its own AAD so a ciphertext from one cookie cannot be opened as another.

| Cookie | Roundtrip | Cross-purpose rejection | Tamper detection |
|--------|:---------:|:----------------------:|:----------------:|
| `ovumcy_auth` | ✓ | ✓ | ✓ (auth-tag, body byte, nonce) |
| `ovumcy_flash` | ✓ | ✓ | †  |
| `ovumcy_recovery_code` | ✓ | ✓ | †  |
| `ovumcy_register_pickup` | ✓ | ✓ | †  |
| `ovumcy_reset_password` | ✓ | ✓ | †  |
| `ovumcy_oidc_auth` | ✓ | ✓ | †  |
| `ovumcy_oidc_stepup` | ✓ | ✓ | †  |
| `ovumcy_oidc_logout_bridge` | ✓ | ✓ | †  |
| `ovumcy_oidc_link_pending` | ✓ | ✓ | ✓  |
| `ovumcy_totp_pending` | ✓ | ✓ | ✓  |
| `ovumcy_totp_setup` | ✓ | ✓ | ✓  |

† AES-256-GCM guarantees tamper detection for all purposes by construction; explicit tests cover
`ovumcy_auth` (three distinct mutation sites), `ovumcy_totp_pending`, `ovumcy_totp_setup`, and
`ovumcy_oidc_link_pending` as representative high-value targets.

Backward-compatibility goldens: `internal/api/secure_cookie_codec_golden_test.go` holds sealed
values produced by the pre-consolidation codec for all eleven purposes, and
`internal/security/field_crypto_golden_test.go` holds AAD-bound and legacy field ciphertexts.
They pin the HKDF labels, AAD construction, envelope, and payload layout; never regenerate the
fixtures to make these tests pass.

## Transparency

The cycle-prediction algorithm is fully documented in
[docs/cycle-prediction.md](docs/cycle-prediction.md), including its assumptions,
limitations, and an explicit medical disclaimer. The worked examples in that
document are mirrored 1:1 by reference tests, so the documentation and the code
cannot silently drift apart. Anyone can read exactly how a prediction is made and
verify it against the numbers.

## Running the suite

```bash
# Go: unit + integration + property + fuzz seeds
go test ./...

# Active fuzzing of a single target
go test ./internal/services/ -run '^$' -fuzz FuzzParseDayDate -fuzztime 30s

# End-to-end (Playwright)
npm run e2e

# Mutation testing (slow; local or nightly)
bash scripts/mutation.sh baseline
```

## Honest limits

- Mutation efficacy will never be 100%: equivalent mutants are unkillable by
  construction, and we refuse to add brittle markup/log-string tests just to move
  a number.
- Predictions are calendar-based estimates, not medical advice or contraception
  (see the disclaimer in the cycle-prediction doc).
