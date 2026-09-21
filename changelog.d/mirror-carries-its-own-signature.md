### Security

- **The Docker Hub mirror now carries a signature that verifies there.** `cosign verify
  docker.io/ovumcy/ovumcy-web:…` — the command SECURITY.md and README.md hand an operator who pulls
  from Docker Hub rather than GHCR — had nothing to read. The mirror is made with `cosign copy`,
  which walks the pre-v3 `sha256-<hex>.sig` layout, while the pinned Cosign v3 attaches a Sigstore
  bundle through the OCI referrers API and writes nothing at that tag: the manifest crossed and the
  signature did not. The image on Docker Hub was the digest this repository signed and attested, so
  the failure was an operator unable to confirm a genuine image rather than an unverified one
  standing as verified — but the documents promised a check that could not pass. The mirror is now
  signed on Docker Hub itself, over that same digest and under the same workflow identity.

  The release job also had the mirror in the wrong order — the aliases were written before the
  signing call, so a failure there would leave `latest` on a public registry with nothing to verify
  it by, which is the defect the publish order exists to refuse. The mirror now follows the order
  GHCR already follows: the bytes cross under no public alias, the digest is signed, that signature
  is read back, and only then is an alias written.
