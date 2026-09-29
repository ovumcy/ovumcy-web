### Security

- **A tag named `origin/main` can no longer stand in for `main` at the release gate.**
  `verify-release-tag` refuses a release tag whose commit `main` does not contain, and it read
  `main` by the short name `origin/main`. Git resolves `refs/tags/<name>` before
  `refs/remotes/<name>`, and the gate's full-history checkout fetches every tag, so pushing a tag
  literally named `origin/main` onto an off-main commit made that commit its own "main" and let
  the ancestry check pass. The gate now reads `refs/remotes/origin/main` by its full name in both
  the lookup and the ancestry test. The release-gate suite runs the step against a fixture origin
  carrying such a tag and requires the refusal to name `main`'s real tip.
