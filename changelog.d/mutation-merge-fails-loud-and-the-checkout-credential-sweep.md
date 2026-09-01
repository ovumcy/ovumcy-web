### Security

- **A mutation-testing shard that silently produced no report can no longer be folded into a
  "complete" combined report.** The per-shard guard that catches a dead shard (OOM, disk
  exhaustion, timeout) only failed that one shard; the merge step downstream ran regardless
  (`if: always()`) and its own upload was `if-no-files-found: ignore`, so a 4-of-5 result still
  published under the same canonical artifact name a complete run uses, with nothing recording the
  gap. `mutationmerge` now takes an `-expect <n>` count — read from the same shard-count registry
  `verify-shards` already checks the partition against — and refuses to merge (fails loudly) when
  fewer shard reports arrive than the registry says are owed.

- **The changelog-fragment gate no longer splices a PR's base-branch name directly into a shell
  command, and no longer goes green having verified nothing.** `git fetch --no-tags origin ${{
  github.event.pull_request.base.ref }}` interpolated an unescaped GitHub Actions expression
  straight into `run:`; it's now a quoted shell variable, matching the pattern this codebase
  already uses elsewhere (`ci.yml`'s `changes` job). The same raw-splice pattern was found, and
  fixed, at the same job's sibling in `ci.yml` (patch-coverage's base-branch fetch) in a companion
  PR. Separately, every real step in the job was gated on `env.RUN_GATE == 'true'` with no
  unconditional fallback — a `RUN_GATE` that ever evaluated to anything else would report success
  having run no check at all. A final step now asserts and fails on that case explicitly.

- **Every `actions/checkout` step across the workflows now sets `persist-credentials: false`.**
  It was present in exactly one of 26 occurrences across 8 files (`scorecard.yml`). None of the
  other 25 push or otherwise need the persisted git token afterward — confirmed no workflow greps
  for `git push`/`git commit`, and the repository is public, so the handful of anonymous
  `git fetch`-after-checkout steps work unauthenticated either way. The sharpest of the 25:
  `security.yml`'s trivy-image job checks out a PR's own ref and then builds and runs a container
  from that PR's own `Dockerfile`; a persisted credential sitting on disk during that step is an
  unnecessary exposure if a future PR changes what the build step actually reads.
