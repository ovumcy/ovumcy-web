### Internal

- CI: a push to `main` now runs the unit and race lanes itself whenever the merge-queue run did not execute them (proven-tree skip), so the release gate finds a successful `test-go-shard (1)` and `race-db (1)` on the tagged commit.
