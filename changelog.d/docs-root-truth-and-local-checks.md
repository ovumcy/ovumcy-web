### Internal

- **The README no longer promises data never leaves your server.** The storage FAQ answered
  "and nowhere else", which the product's own optional features falsify: a webhook reminder
  POSTs the predicted period/ovulation dates to whatever URL the owner configures, and a
  calendar app subscribed to the `.ics` feed pulls those dates onto its own servers. The
  answer now names both paths as owner-enabled egress instead of denying them.
- **The README's CI sentence describes the events CI actually runs on.** It said staticcheck,
  vet, tests and the frontend build run "on pushes and pull requests"; the push lane on `main`
  clears `run_core` precisely because the merge queue already ran that suite on the same
  commit, and executes only the cross-browser e2e, image-smoke and Postgres-smoke lanes the
  queue skips.
- **`Go 1.26+` is now `Go 1.26.6+` in both places** (badge and Requirements) — `go.mod` requires
  `go 1.26.6`, which bites a `GOTOOLCHAIN=local` toolchain below that patch.
- **`CONTRIBUTING.md`'s local check list runs what CI's frontend lane runs.** It listed
  `npm run lint:js` and `npm run build` only, so a type error or a JS unit failure —
  `lint:types` and `test:unit`, both required in CI — went green locally and red in CI.
- **The untranslated-string instruction names a mechanism that can exist.** It asked for a
  `TODO(<locale>)` inside the locale file; those files are strict JSON parsed at boot, so a
  comment breaks startup, an added key fails `TestLocaleKeysParity`, and a marker inside the
  value stops being the verbatim English string the same sentence demands. Copied keys are
  listed in the pull request description instead.
- **The Dependabot changelog exemption names a real assembly step.** It said the
  `### Dependencies` section "is assembled from the dependency bumps that merged" — no such
  mechanism exists; `changelogd assemble` reads fragments and the frozen `[Unreleased]` body
  and nothing else. The section is compiled at release time from `git log <last-tag>..HEAD`
  over the Dependabot merges and entered as one fragment before assembly.
- **`TESTING.md`'s stale-profile warning attributes the false pass to its real cause.** It
  blamed Go's test result cache, which cannot produce the effect: the cache is keyed on the
  content-hashed test binary, every production file is an input under the documented
  `-coverpkg`, and the documented command passes `-count=1` anyway. `patchcov` reads whatever
  `coverage.out` is on disk without checking how it was produced — a profile from before the
  edit, or over a narrower package set, is what passes lines no test ran.
- **Mutation testing is weekly in the two places that still said nightly** (`TESTING.md`'s
  command comment and `scripts/mutation.sh`'s mode help); the workflow runs Mondays at
  04:23 UTC and the same file says weekly twice already.
- **The efficacy history no longer reads as if all three packages fell from ~99%.** Only
  `internal/services` was ever there; `internal/security` (93.4%) and `internal/api` (79.2%)
  rose to their current figures through the hardening pass.
