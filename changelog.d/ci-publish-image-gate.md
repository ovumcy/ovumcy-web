### Fixed

- **`ghcr.io/ovumcy/ovumcy-web:latest` follows `main` again.** The CI job that publishes it was
  skipped on every push to `main` while the run itself reported green, so `latest`, `main` and the
  newest `sha-…` tag all still pointed at one image 242 commits old, and no `sha-<commit>` tag had
  been created for any commit since. The job's condition named only the event and the branch, and
  a condition holding no status-check function is given an implicit `success()` — which is false
  once an ancestor job was skipped. On a push to `main` every lane below the five gates the
  publish depends on is deliberately skipped, because the merge queue has already run them on that
  exact commit, so the publish was skipped with them. The condition now overrides that implicit
  predicate and instead requires each of the five gates — `test`, `race`, `e2e`,
  `e2e-postgres-smoke` and `image-smoke` — to have reported `success` by name, so a gate that
  fails still blocks the publish. A documentation-only push, where the image smoke test does not
  boot the image, publishes nothing on purpose and leaves `latest` on the previous commit, which
  carries the identical image.
