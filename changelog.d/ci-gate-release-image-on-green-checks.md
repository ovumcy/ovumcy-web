### Security

- **A release tag no longer publishes an image on its own say-so.** Pushing `v*` starts no CI run —
  CI is triggered by pushes to `main` — so the tag went straight to build-and-push with only the
  pre-publish vulnerability scan between it and the registry. A tag on a commit that never merged,
  or on one whose checks went red, produced a signed, attested release image regardless. The publish
  workflow now runs a gate first: the tagged commit must be contained in `main`, and the same five
  checks that `latest` waits on — `test`, `race`, `e2e`, `e2e-postgres-smoke` and `image-smoke` —
  must have passed on the run that judged it on `main` itself. A merge-queue run's verdict is not
  accepted in their place: it does not run the image or Postgres smokes at all. A tag that fails
  either condition fails the workflow and publishes nothing; there is no override. Publishing
  `latest` from `main` is unchanged — that path already waited on these checks.

### Fixed

- **The release image is built by the Go toolchain the test suite actually runs.** The builder stage
  had been bumped to `golang:1.27rc2-alpine3.24` while every CI job kept resolving its toolchain
  from `go.mod` (1.26.6), and `GOTOOLCHAIN` only ever upgrades — so the published binary was
  compiled by a release candidate that no unit, race or browser test had ever run under. The builder
  is pinned back to `golang:1.26.6-alpine3.24`. CI now fails when the builder tag and the `go.mod`
  directive disagree. Dependabot still offers a pre-release bump of that image — a docker `ignore`
  condition takes version requirements and cannot express "no `rc` tags", and an invalid one would
  stop Dependabot for every ecosystem in the repository — so this CI check is what catches it,
  documented next to the `dependabot.yml` entry it explains.
