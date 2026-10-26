### Changed

- **`latest` now names the newest stable release, not the tip of `main`.** `ghcr.io/ovumcy/ovumcy-web:latest`
  and its Docker Hub mirror used to be rewritten by every green commit on `main`, so an operator who
  pulled the floating tag ran an unreleased build. A push to `main` now publishes only `:main` and
  `sha-<short>`; `:main` is the moving pre-release channel. `:latest` moves only when a release tag
  is published, and only for a stable `vMAJOR.MINOR.PATCH` tag that is the highest one in the
  repository: a pre-release such as `v1.10.0-rc.1`, a backported older patch, or an older tag pushed
  again leaves it where it is. It is written by the same promotion, after the same signature check,
  as every other tag of the release, and mirrored to Docker Hub in the same order, so the signing
  and verification commands in `SECURITY.md` apply to it unchanged.

  A manually dispatched `promote-latest` workflow points `latest` at an existing stable release
  without a rebuild. It resolves the release's digest, verifies the cosign signature and the
  provenance attestation issued over that digest, and checks the Docker Hub mirror holds the same
  signed digest before it writes either registry.

  Pin a release tag in `OVUMCY_IMAGE` as before; `:main` is not for production.
