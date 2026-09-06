### Security

- **A server that ran without a calendar-feed restore fence now says so in its own database, and the
  first start with a fence acts on it.** Until now, a database that had only ever run unfenced looked
  exactly like a brand-new one — both halves of the fence empty — so mounting the fence for the first
  time adopted whatever feeds it found. An owner who revoked their `.ics` subscription on the unfenced
  instance, on a database later restored from a backup taken before that revocation, got the old
  subscribe URL back and nothing recorded it.
- **Operators:** the first start after you mount the fence on an instance that had been running without
  one disarms every armed calendar feed once, and the startup line says that is what happened. It is
  expected; owners re-generate their subscribe URLs from `Settings → Calendar feed`, and later starts
  disarm nothing.
