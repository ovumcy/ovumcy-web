### Added

- **The runtime image is now mirrored to Docker Hub, and the README carries its pull count.** The
  image published to `ghcr.io/ovumcy/ovumcy-web` is copied to `docker.io/ovumcy/ovumcy-web` under
  the same tags, so an operator whose environment pulls from Docker Hub can run the same release
  without a registry mirror of their own. The copy is content-addressed: it moves the manifest
  bytes and the Cosign signature that already covers them, never a second build, so both registries
  answer with the one digest this project signed. The README and the security policy now name the
  Docker Hub reference beside the GHCR one and say that every verification command takes either,
  because the build provenance is issued against the digest rather than against a registry and the
  signature is copied along with the manifest it covers.

  Nothing reaches Docker Hub until the GHCR release is finished — signed, attested, read back, and
  every public GHCR tag resolved anonymously to the signed digest — and the mirror is not reported
  published until every mirrored tag has been resolved anonymously to that same digest and the
  signature has been found on Docker Hub beside it. A failure in the mirror cannot affect the GHCR
  release, which by then already exists in full.

  The mirror uses its own publishing credential and skips entirely when that credential is absent,
  so a fork or a clone publishes to GHCR exactly as before. The credential that authenticates
  base-image pulls is deliberately not reused: it is handed to jobs that run on every pull request,
  and write access to a public registry does not belong there.
