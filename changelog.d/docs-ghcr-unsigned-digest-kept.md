### Changed

- **The README now says what a refused publish leaves in GHCR.** The release workflow pushes the
  image by digest, scans every platform, and only then signs and tags it, so a refused scan leaves
  an untagged, unsigned digest in the public package. The image-verification section now states that
  this digest can exist, why it is safe to ignore (the Cosign check fails on it), and why it is not
  deleted: that needs delete rights over the whole package inside the publish job, which would let a
  compromised publish step delete signed releases too.
