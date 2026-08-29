### Internal

- **The changelog-fragment gate now passes dependency bumps without a fragment.**
  The check stays required for every pull request and keeps its `merge_group`
  trigger; it returns success immediately when the pull request's author is
  `dependabot[bot]`, which cannot write a fragment. Dependency updates therefore
  no longer reach `changelog.d/`. At release time the operator compiles a
  release's `### Dependencies` section by hand from `git log <last-tag>..HEAD`
  over the dependabot merges and enters it as one fragment before running
  `changelogd assemble`, which itself reads only fragments and the frozen
  `[Unreleased]` body.
