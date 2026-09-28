### Changed

- **The README now says what a refused publish leaves in GHCR.** The release workflow pushes the
  image by digest, scans every platform, and only then signs and tags it, so a refused scan leaves
  an untagged, unsigned digest in the public package. The image-verification section now states that
  this digest can exist, why it is safe to ignore (the Cosign check fails on it), and why it is not
  deleted: the workflow token cannot delete package versions, and a token that could would be a new
  long-lived secret in the publish path.
