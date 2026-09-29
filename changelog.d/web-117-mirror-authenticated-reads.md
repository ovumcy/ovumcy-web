### Internal

- **The Docker Hub mirror's aliases are copied from GHCR, so they no longer spend Docker Hub's pull
  quota.** Each alias used to be copied from the mirror's own digest, and one `cosign copy` reads
  about fourteen manifests from its source: the index, each platform, and a signature, attestation
  and SBOM tag probe per entity. The aliases now read the same digest from GHCR, whose bytes the
  first copy has already landed on Docker Hub, so Docker Hub receives only the manifest under each
  tag and the HEADs before it, which its quota does not count. A new informational step after the
  mirror login reports whether the Docker config holds an entry for Docker Hub and what Docker Hub
  answers for the rate limit anonymously and with that login: the limit, what remains, and the
  source it charges. The step never fails the job, and the credential reaches `curl` on stdin only.
  `scripts/publishorder` and `scripts/releasegate` pin the new alias source and the step's place
  in the mirror's sequence.
