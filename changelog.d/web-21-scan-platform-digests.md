### Security

- **The vulnerability scan that gates a published image now judges every platform of the digest
  that gets signed.** It used to scan a separate single-platform rebuild: `linux/amd64` only, from
  bytes that shared a build cache with the pushed image but were never shown to be it, so the
  `linux/arm64` image was published without a scan of its own. The publish job now pushes the image
  under no tag, scans each platform's manifest out of that exact digest with the same scanner,
  threshold and exit code as the required image-scan check, and only then signs, attests and tags
  it. A finding on any platform leaves the digest unsigned and with no public tag. The platform list
  is declared once and read by both the build and the scan, so a platform added to one cannot miss
  the other.
