### Security

- **A published image tag now exists only once the image under it is signed.** Every release image
  is Cosign-signed and carries a SLSA build-provenance attestation and an SBOM, and the verification
  commands in the README and the security policy are the way to check it. The publish job produced
  that state in the wrong order: it pushed the image under its public tags — `vX.Y.Z`, `latest`,
  `main`, `sha-…` — and installed Cosign, signed and attested afterwards, with a check that the
  image pulls anonymously in between. Anything failing after the push left the workflow red and the
  release already resolvable by anyone, unsigned and without provenance. A red workflow retracts
  nothing; nothing deletes a tag that already answers.

  The image is now pushed by digest and given no tag at all. It is signed and attested at that
  digest, both are read back from the registry, and only then are the public tags written — from the
  manifest bytes that already hash to the signed digest, so a tag cannot come to point at anything
  else. Every tag is then checked to be anonymously pullable and to resolve to that same digest.
  What changes is that no failure can produce a tag pointing at an unsigned image: one before the
  tags are written leaves an untagged manifest and no public alias at all, and one during the write
  can leave the aliases written before it — each of them resolving to the digest that was signed.

  Verifying a release is the same procedure it was, against the same tags, and both documented
  commands were tightened: the signer-identity pattern now escapes the dot in `github.com` as it
  already escaped the two after it, and the security policy checks the build provenance against this
  repository rather than against every repository in the account. An operator holding a copy of the
  older commands is running a looser check than this release documents.
