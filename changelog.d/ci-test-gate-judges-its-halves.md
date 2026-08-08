### Internal

- **The `test` gate reports a verdict instead of disappearing when a unit half fails.** The job had
  no `if`, so a failed dependency skipped it — and GitHub reports a skipped job as a satisfied
  required check. Measured on a throwaway branch carrying one deliberately failing Go test:
  `test-go` failed, and `test`, `e2e` and `patch-coverage` — three of the fourteen required
  contexts — all reported SKIPPED. `race` gates on the `changes` job rather than on `test`, so it
  ran and passed, and nothing else covered the unit suite. The job now runs under `!cancelled()`
  and turns its two dependencies' results into its own verdict: `success` and `skipped` pass —
  the latter is the docs-only decision not to run the battery — and anything else fails the
  context, so a failing unit suite is red where branch protection looks.
