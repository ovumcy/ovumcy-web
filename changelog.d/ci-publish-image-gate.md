### Fixed

- **`ghcr.io/ovumcy/ovumcy-web:latest` follows `main` again.** The CI job that publishes it was
  skipped on every push to `main` while the run itself reported green, so `latest`, `main` and the
  `sha-…` tag they share all still pointed at one image 242 commits behind the head of `main`, and
  the head carried no tag of its own. The job's condition named only the event and the branch, so
  it rested on the predicate GitHub supplies when a condition names no job status of its own. On a
  push to `main` every lane below the five gates the publish depends on is deliberately skipped,
  because the merge queue has already run them on that exact commit, and the publish was skipped
  with them. The condition now names what it needs instead of resting on that implicit
  predicate: each of the five gates — `test`, `race`, `e2e`,
  `e2e-postgres-smoke` and `image-smoke` — to have reported `success` by name, so a gate that
  fails still blocks the publish. A documentation-only push, where the image smoke test does not
  boot the image, publishes nothing on purpose and leaves `latest` on the previous commit, which
  carries the identical image.
