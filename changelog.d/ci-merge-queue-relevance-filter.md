### Internal

- **Merge-queue validation runs are judged by the same docs/meta relevance diff as pull requests.**
  The `changes` job only computed a diff for `pull_request`; every other event, the merge queue's
  own validation run included, fell into the catch-all branch and paid the full battery. So a
  change already judged irrelevant on its own pull-request run still ran `test-go`,
  `test-frontend` and `race` a second time inside the queue — `race` being the lane the queue's
  60-minute status-check timeout is measured against. The queue branch is its base plus every
  pull request batched into the group, so one diff against `merge_group.base_sha` judges the
  whole batch. Push, release and `workflow_dispatch` still run in full: the post-merge safety net
  is unchanged. Undetermined always means run — an unknown event, a missing or unreachable base,
  a failed diff and an empty file list are each written out as their own branch, and the
  consuming jobs repeat the fail-safe once more (`!= 'false'`, plus `!cancelled()`), so a
  detection job that dies can no longer skip a required check into a satisfied state.
