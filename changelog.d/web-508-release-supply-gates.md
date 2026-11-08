### Changed

- **Published image tags are written once.** The publish workflow reads a `v*` or `sha-*` tag on
  GHCR before writing it and refuses when it already names another digest, so re-running a release
  run fails instead of re-pointing a verified tag. A release no longer writes `sha-<short>`: that
  tag belongs to the `main` publish of the same commit.
- **The Docker Hub mirror is signed before any tag names it.** The first copy goes to Docker Hub by
  digest with `oras`, which writes no tag. The mirror is signed and its signature read back, and
  only then are the release tags (and the `sha256-<hex>` tags `cosign copy` adds) written.

### Internal

- The release gate no longer accepts a skipped `trivy-fs` from the merge-queue run. It looks for
  the run that scanned the tree: the commit's push run, or a first-parent ancestor whose manifests
  are unchanged. If none succeeded, it refuses.
- The jobs that read the Docker Hub publishing credential run in the `release` environment.
- `e2e-postgres-smoke` runs on `postgres:18.4-alpine3.22`, the image the example stacks pin. A guard
  fails when their Postgres majors differ, and the e2e runner mounts its tmpfs at the data path the
  image's major uses.
