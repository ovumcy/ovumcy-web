### Changed

- **Changelog entries now accumulate as per-pull-request fragments.** Each pull
  request adds one file under `changelog.d/` instead of editing the
  `[Unreleased]` section of `CHANGELOG.md`; a CI check enforces the fragment and
  `go run ./scripts/changelogd assemble` folds the accumulated fragments into
  `CHANGELOG.md` when a release is cut. Entries written before this change stay
  under `[Unreleased]` until the next release consumes them.
