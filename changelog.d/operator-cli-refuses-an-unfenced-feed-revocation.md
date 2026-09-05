### Security

- **`ovumcy users delete` and `ovumcy reset-password` now refuse a calendar-feed revocation they
  cannot record outside the database, instead of completing it anyway.** The restore fence
  (previous entry) advances on every write that arms, rotates or removes a feed, but the operator
  CLI's own writes only ever advanced the database half: an operator's shell rarely shares the
  server's `CALENDAR_FEED_FENCE_PATH`, so the file half stayed behind. A restore of the backup
  taken just before such a revocation then agreed with that unmoved file, and the feed the operator
  believed gone served the calendar again — the exact defect the fence exists to close, reintroduced
  through the one write path that never confirmed it. A second, narrower window let the same gap
  mask a genuine restore: an operator running the CLI with a *working* anchor, after a restore had
  landed but before the server had booted against it, minted a token that resynchronized the two
  halves, so the boot that followed read an ordinary restart instead of the restore that had just
  happened.

  Both commands now confirm the two halves already agree before advancing them, and refuse — the
  account or the password left exactly as it was, nothing recorded on either half — when they do
  not: no path configured, an unmounted volume, a file and marker that disagree, or a fence neither
  half has ever recorded. The refusal names `CALENDAR_FEED_FENCE_PATH`, what continuing anyway would
  have cost, and what to run instead. Run either command through `docker compose exec` on the
  running container, or against the same fence path the server uses, to reach it. Full contract:
  `docs/self-hosted.md → Calendar Feed Restore Fence`.
