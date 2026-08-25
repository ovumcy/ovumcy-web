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
| **End-to-end** | Real user flows in a browser; full suite on Chromium, cross-engine smoke spec on Firefox and WebKit | `e2e/*.spec.ts` (Playwright) |
| **Property-based** | Invariants of the cycle math over thousands of generated inputs | `internal/services/cycles_property_test.go` (`pgregory.net/rapid`) |
| **Fuzz** | Robustness of parsers/validators against arbitrary/invalid input | `internal/services/policy_fuzz_test.go` (native Go fuzzing) |
| **Reference vectors** | Cycle predictions match the documented algorithm, number for number | `internal/services/cycles_reference_test.go` |

The Go suite runs to thousands of test functions across `internal/`, and the
browser suite is every `e2e/*.spec.ts` file — `playwright.config.ts` declares
only `testDir: 'e2e'`, with no `testMatch` or `testIgnore`, so the file listing
*is* the suite (full suite on Chromium; cross-engine smoke on Firefox and
WebKit). Exact figures are deliberately not written here: a number in prose
goes stale on the next batch of tests and nothing re-derives it. Ask the tree
instead:

```bash
grep -rE '^func Test[A-Z]' --include='*_test.go' internal | wc -l
find e2e -name '*.spec.ts' | wc -l
```

Tests favor behavior and persisted state over markup or implementation details.

## We test our tests

High coverage proves code *ran*, not that a test would *fail if the code broke*.
We close that gap with **mutation testing** ([gremlins](https://github.com/go-gremlins/gremlins)):
it injects faults into the production code and checks that at least one test
fails ("kills" the mutant). Surviving mutants reveal weak assertions.

- Run it locally: `scripts/mutation.sh baseline` (full) or `scripts/mutation.sh diff <ref>` (changed code only).
- A weekly CI job tracks the trend; it is advisory and never blocks a merge.
- Baseline scope now covers business-logic, security, and transport: `internal/services`, `internal/security`, and `internal/api`.
- **Mutation efficacy** (gremlins, killed / (killed + survived); tracked weekly): `internal/services` **93.3%** (1529/1638), `internal/security` **97.8%** (134/137), and `internal/api` **97.3%** (649/667), measured on a clean-Linux CI run. Efficacy dipped from an earlier ~99% as v1.8.0 landed large new subsystems (webhooks, `.ics` feed, reminders) whose mutants were then triaged. An exhaustive per-mutant pass re-verified **every** survivor against a broad covering suite — the coverage-guided run over-reports survivors (Go leaves `const`/`case` lines uninstrumented, and its test selection can skip the killing test, so a mutant an existing test already kills can still show as survived) — closing the genuine gaps and documenting the residual as equivalents or coverage-attribution artifacts. `internal/api` is the slowest package to mutate — not the largest (`internal/services` has more non-test source lines) but the one whose tests are heavy DB integration — and it exceeds CI's 3h job timeout unsharded, so it runs as 5 file-subset shards (`internal_api_1`..`5`, a deterministic partition of the package's own files — see `scripts/mutation.sh`) merged into one `internal_api.json`. `internal/services` is sharded 5 ways on the same mechanism. Canonical efficacy comes only from the weekly clean-Linux job, never a local Windows run; per-package breakdowns live in [`.mutation/`](.mutation/).
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

All twelve AEAD-sealed cookie purposes are exercised by `internal/api/secure_cookie_codec_security_test.go`.
Each purpose is bound to its own AAD so a ciphertext from one cookie cannot be opened as another.

| Cookie | Roundtrip | Cross-purpose rejection | Tamper detection |
|--------|:---------:|:----------------------:|:----------------:|
| `ovumcy_auth` | ✓ | ✓ | ✓ (auth-tag, body byte, nonce) |
| `ovumcy_flash` | ✓ | ✓ | †  |
| `ovumcy_recovery_code` | ✓ | ✓ | †  |
| `ovumcy_calendar_feed` | ✓ | ✓ | †  |
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
values produced by the pre-consolidation codec for the eleven purposes that predate the
consolidation — `ovumcy_calendar_feed` was minted later and has no such value to pin — and
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
# Go: unit + integration + property + fuzz seeds.
# Scoped to the module's Go trees (not ./...) so a local node_modules/ — where a
# vendored JS dependency ships a .go file — isn't swept into the wildcard.
# -timeout 20m declares the same budget CI does: Go's default is 10 minutes PER
# PACKAGE, and internal/api's DB-integration suite sits at that edge, so the
# default run dies as `panic: test timed out after 10m0s` with a goroutine dump
# that reads like a product bug. Read that panic as the budget, not as a defect.
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... -timeout 20m

# Active fuzzing of a single target
go test ./internal/services/ -run '^$' -fuzz FuzzParseDayDate -fuzztime 30s

# End-to-end (Playwright)
npm run e2e

# Mutation testing (slow; local or nightly)
bash scripts/mutation.sh baseline
```

## Checking patch coverage locally

`scripts/patchcov` is the same gate CI's `patch-coverage` job runs: every modified,
coverable Go line in your diff against `origin/main` must be exercised by a test
(a genuinely unreachable line is excluded with a trailing `// codecov:ignore`, see
the comment at the top of `scripts/patchcov/main.go`).

**Warning: running `patchcov` against a stale `coverage.out` gives a false pass.**
`go test -coverprofile` is subject to Go's test result cache — if you edit a file
and re-run the coverage command without also touching its test, `go test` can
silently reuse a cached run from *before* your latest edit. `coverage.out` then
reflects the old code, `patchcov` reports your newest lines as covered, and CI
(which always starts from a clean checkout with an empty test cache) fails on the
same diff. This has bitten contributors more than once — always regenerate the
profile fresh before trusting a local "gate OK".

This isn't enforced by a local hook — CI's `patch-coverage` job is the gate,
run on every PR. To check before pushing, reproduce CI's coverage condition
end to end. The two steps that matter are `go clean -testcache` and
`-count=1`; either alone defeats the cache, run both for good measure:

```bash
rm -f coverage.out
go clean -testcache
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... \
  -timeout 20m \
  -coverprofile=coverage.out -covermode=atomic \
  -coverpkg=./cmd/...,./internal/...,./migrations/...,./scripts/...,./web/... \
  -count=1
COVERAGE_FILE=coverage.out BASE_REF=origin/main go run ./scripts/patchcov
```

## Honest limits

- Mutation efficacy will never be 100%: equivalent mutants are unkillable by
  construction, and we refuse to add brittle markup/log-string tests just to move
  a number.
- Predictions are calendar-based estimates, not medical advice or contraception
  (see the disclaimer in the cycle-prediction doc).
