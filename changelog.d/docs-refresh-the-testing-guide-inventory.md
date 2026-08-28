### Internal

- **The documented local Go command now declares the timeout budget CI declares.**
  All four copies of `go test ./cmd/... ./internal/... ./migrations/... ./scripts/...
  ./web/...` — `TESTING.md`'s run block and its patch-coverage recipe, `CONTRIBUTING.md`,
  and `README.md` — pass `-timeout 20m`. Go's default is 10 minutes *per package* and
  `internal/api`'s DB-integration suite runs at that edge, so the documented command
  aborted with `panic: test timed out after 10m0s` and a goroutine dump that reads like
  a product bug; the CI workflow has budgeted every one of its own `go test` calls since
  the same measurement, and the docs handed contributors the unbudgeted spelling.
- **`TESTING.md` no longer states test counts that go stale.** The header said "2,300+ Go
  test functions across `internal/` and 29 Playwright specs"; the tree holds 2,720 and 33,
  and the spec figure had already been corrected once. Both numbers are replaced by the
  two commands that re-derive them, plus the fact that makes the spec listing authoritative
  (`playwright.config.ts` sets only `testDir: 'e2e'`, with no `testMatch`/`testIgnore`).
  The mutation section's "`internal/services` is some 40% bigger in source lines" —
  measured at 18,582 non-test lines against `internal/api`'s 11,453, so nearer 62% — loses
  the percentage for the ordering claim the sentence actually rests on.
