none

CI-only: the merge queue's `changes` job now skips the core Go/race/frontend
lanes and the Playwright e2e shards on a `merge_group` run whose tree the
queued PR's head already ran `ci.yml`'s `pull_request` suite to success on,
with that run and that head bound to exactly this PR against the queue's own
base branch and the queue base an ancestor of the head. A batch qualifies only
when the PRs ahead of that one are already in its head (a stacked PR). Such a
commit sends Codecov no upload for `main`. A head that ran green against
another base and reaches this one without a new push (retarget, reopened
branch) is not caught.
