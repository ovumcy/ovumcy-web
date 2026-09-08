### Security

- **A start without a usable calendar-feed restore fence now drops the database half of the fence
  marker as well as stamping the database.** An instance that WAS fenced and then lost
  `CALENDAR_FEED_FENCE_PATH` for a start — while its data volume, and the fence file on it, were kept
  — left both halves holding the same token: nothing advances either half while no fence is
  configured. A backup taken in that gap, restored beside the untouched file, therefore compared
  equal, the boot read continuity, and the stamp — consulted only where both halves are empty — was
  never reached. An owner who revoked their `.ics` subscription during the gap got the old subscribe
  URL back, serving again.
- **Operators:** the first start that has the fence back after a start without it disarms every armed
  calendar feed once — including feeds armed while the fence was missing — and then re-arms; the
  startup line names it. Owners re-generate their subscribe URLs from `Settings → Calendar feed`, and
  later starts disarm nothing. Until that start, `ovumcy users delete` and a forced
  `ovumcy reset-password` refuse, because the marker they confirm is not there yet.
