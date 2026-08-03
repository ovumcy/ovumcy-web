### Internal

- **The changelog-fragment gate now passes dependency bumps without a fragment.**
  The check stays required for every pull request and keeps its `merge_group`
  trigger; it returns success immediately when the pull request's author is
  `dependabot[bot]`, which cannot write a fragment. Dependency updates therefore
  no longer reach `changelog.d/`, and a release's `### Dependencies` section is
  assembled from the bumps that merged rather than from fragments.
