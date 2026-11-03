### Fixed

- **A calendar-feed rotation that fails to save no longer loses the pending reveal of the link
  still in use.** When the new token could not be stored, the response cleared the one-time reveal
  cookie the browser held for the previous, still-serving link, so a URL its owner had not yet seen
  was gone and only another rotation could show one. The failed rotation now leaves that cookie as
  it was.

### Changed

- **Self-hosting names two fence setups it does not support.** Two or more server replicas against
  one database, and an operator command whose fence advance interleaves with the server's own, can
  each leave the restore fence's two halves disagreeing, which the next start reads as a restore.
  `docs/self-hosted.md` now says so and asks for one server process per database.
