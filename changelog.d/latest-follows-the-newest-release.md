### Changed

- **`latest` now names the newest stable release, not the tip of `main`.** `ghcr.io/ovumcy/ovumcy-web:latest`
  and its Docker Hub mirror used to be rewritten by every green commit on `main`, so an operator who
  pulled the floating tag ran an unreleased build. A push to `main` now publishes only `:main` and
  `sha-<short>`; `:main` is the moving pre-release channel. `:latest` moves only when a release tag
  is published, and only for a stable `vMAJOR.MINOR.PATCH` tag that is the highest one in the
  repository: a pre-release such as `v1.10.0-rc.1`, a backported older patch, or an older tag pushed
  again never moves it. The image metadata step no longer adds `latest` on its own for any tag event,
  and the publish run asks again whether its tag is still the highest right before it writes `latest`
  on GHCR and on Docker Hub, so a higher release tagged during the build keeps the alias. It is
  written by the same promotion, after the same signature check, as every other tag of the release,
  so the signing and verification commands in `SECURITY.md` apply to it unchanged.

  A manually dispatched `promote-latest` workflow points `latest` at an existing stable release
  without a rebuild, and only at the highest stable tag: a lower tag is refused, with no override. It
  resolves the release's digest, verifies the cosign signature and the provenance attestation issued
  over that digest (pinned to the release's own workflow and tag ref), and, when the Docker Hub
  mirror lacks the release, copies it there byte for byte — the index, platform manifests, blobs and
  the release's own signature bundle, so the digest is unchanged and the copy verifies under the
  release's signer identity; nothing is re-signed. The copy is idempotent and runs on every
  dispatch, so a mirror left with the index but without the signature bundle by an earlier attempt
  is completed rather than wedged. The copy is verified on Docker Hub before any tag moves on either
  registry. Only then are the release tag (if the mirror lacked it) and `latest` written — on Docker
  Hub first and on GHCR last, so a mirror failure leaves GHCR's `latest` where it was. Docker Hub's
  rate limit is asked again within a bounded budget at each of its calls; any other refusal ends the
  step. A failed run prints where `latest` points on each registry.

  Pin a release tag in `OVUMCY_IMAGE` as before; `:main` is not for production.
