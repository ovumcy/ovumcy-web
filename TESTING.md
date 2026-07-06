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

Currently **1,375+ Go test functions** across `internal/` and **25 Playwright
specs** (full suite on Chromium; cross-engine smoke on Firefox and WebKit).
Tests favor behavior and persisted state over markup or implementation details.

## We test our tests

High coverage proves code *ran*, not that a test would *fail if the code broke*.
We close that gap with **mutation testing** ([gremlins](https://github.com/go-gremlins/gremlins)):
it injects faults into the production code and checks that at least one test
fails ("kills" the mutant). Surviving mutants reveal weak assertions.

- Run it locally: `scripts/mutation.sh baseline` (full) or `scripts/mutation.sh diff <ref>` (changed code only).
- A weekly CI job tracks the trend; it is advisory and never blocks a merge.
- Baseline scope now covers business-logic, security, and transport: `internal/services`, `internal/security`, and `internal/api`.
- **Mutation efficacy** (gremlins, killed / (killed + survived); tracked weekly): `internal/services` **99.7%** (1462/1466) and `internal/security` **91.7%** (99/108). Every surviving mutant is a documented equivalent (an unreachable guard, a redundant clamp, an error-text-only difference) or an OIDC provider path covered by the e2e lanes rather than Go units — classified, not chased. `internal/api` (the largest package, ~8.5k source lines, heavy DB-integration tests) joined the baseline scope but has no number yet: a single unsharded run exceeds CI's 3h job timeout before finishing, so CI runs it as 5 file-subset shards (`internal_api_1`..`5`, a deterministic partition of the package's own files — see `scripts/mutation.sh`) and merges their JSON into one `internal_api.json`. It is baseline pending the first weekly CI run under that sharded scheme, since canonical efficacy comes only from that clean-Linux job, never a local Windows run. Per-package breakdowns live in [`.mutation/`](.mutation/).
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
# Go: unit + integration + property + fuzz seeds.
# Scoped to the module's Go trees (not ./...) so a local node_modules/ — where a
# vendored JS dependency ships a .go file — isn't swept into the wildcard.
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/...

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

The one-liner that reproduces CI's coverage condition end to end:

```bash
bash scripts/patch-coverage-local.sh
```

It removes any existing `coverage.out`, runs `go clean -testcache`, regenerates
the profile with the exact package set and flags CI uses
(`-covermode=atomic -count=1`), then runs `scripts/patchcov` against it. It takes
a few minutes — that's the point, it is the real test suite run for real.

If you'd rather run it by hand, the two steps that matter are `go clean
-testcache` and `-count=1`; either alone defeats the cache, but the script uses
both for good measure:

```bash
rm -f coverage.out
go clean -testcache
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... \
  -coverprofile=coverage.out -covermode=atomic \
  -coverpkg=./cmd/...,./internal/...,./migrations/...,./scripts/...,./web/... \
  -count=1
COVERAGE_FILE=coverage.out BASE_REF=origin/main go run ./scripts/patchcov
```

## Enforcing patch coverage before you push (pre-push hook)

Running `patch-coverage-local.sh` by hand only helps if you remember to do it —
and the stale-`coverage.out` false-pass trap above has bitten contributors more
than once even when they *did* remember. `scripts/hooks/pre-push` closes that
gap by running the check automatically as part of `git push`.

**Enable it once per clone:**

```bash
bash scripts/setup-hooks.sh
```

This points git's `core.hooksPath` at `scripts/hooks` (committing a file under
that path does not make git run it — `core.hooksPath` is what wires a
version-controlled hook directory up to git's actual hook dispatch) and marks
`scripts/hooks/pre-push` executable. Equivalent one-liner, if you'd rather
configure it yourself: `git config core.hooksPath scripts/hooks` (then `chmod
+x scripts/hooks/pre-push` — this repo pins `core.fileMode=false`, so a fresh
checkout may not carry the executable bit git needs to run the hook directly).

**What it does on every `git push`:**

1. Reads the ref range being pushed (git feeds this to the hook on stdin: `<local
   ref> <local sha1> <remote ref> <remote sha1>` per updated ref) and diffs it
   for `*.go` changes.
2. **No Go files changed** (e.g. a docs-only push): skips immediately, no
   coverage run.
3. **Go files changed**: runs `scripts/patch-coverage-local.sh` — the exact
   same fresh, cache-defeated gate described above — and blocks the push
   (non-zero exit) if it fails, printing the uncovered `file:line` entries
   `scripts/patchcov` reports and how to fix them (add a test, or annotate a
   genuinely unreachable line with `// codecov:ignore`).

It is bounded but not instant: expect it to take as long as the full test
suite (a few minutes), since that is the only way to get a trustworthy
answer. The hook prints a notice before it starts so it doesn't look hung.

**Emergency bypass:** `git push --no-verify` skips the hook entirely (git's
own built-in escape hatch). Use it sparingly — CI's `patch-coverage` job will
still enforce the same gate on the PR, so a bypassed push only defers the
failure, it doesn't avoid it.

## Honest limits

- Mutation efficacy will never be 100%: equivalent mutants are unkillable by
  construction, and we refuse to add brittle markup/log-string tests just to move
  a number.
- Predictions are calendar-based estimates, not medical advice or contraception
  (see the disclaimer in the cycle-prediction doc).
