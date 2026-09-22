none

CI-only: the merge queue's `changes` job now skips the core Go/race/frontend
lanes and the Playwright e2e shards on a `merge_group` run whose one queued PR
already ran `ci.yml`'s `pull_request` suite to success on this identical tree,
with that run and that head bound to exactly this PR against the queue's own
base branch. A PR retargeted onto that base without a new push is not caught.
