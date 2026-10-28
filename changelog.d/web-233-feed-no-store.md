### Security

- **A rotated or revoked calendar link no longer keeps answering from a browser's cache.** The
  `.ics` feed was served cacheable for an hour, so a browser that had opened the old link could
  show the old feed for up to an hour after the link was replaced or turned off. The feed is now
  never stored.
