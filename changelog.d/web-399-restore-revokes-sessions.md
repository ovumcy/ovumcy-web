### Security

- **Restoring a backup no longer brings back sessions that were signed out, or webhook endpoints
  that were removed — and the restore fence is now required.** A sign-out everywhere, a password
  change or reset, an operator reset, an account deletion and a webhook removal are all recorded
  in columns a database restore rolls back, so restoring a backup taken before one of them made
  every sign-in cookie minted before the backup point valid again and sent reminders to the
  removed endpoint. The first start after a restore already disarmed calendar feeds; it now also
  signs every owner out and clears every stored webhook endpoint. Owners sign in again and
  re-enter their webhook URL in Settings; their reminder choices are kept. A lost or recreated
  fence volume reads as a restore and costs the same.

  **Upgrading: a v2.0 deploy without the fence mount stops at boot.** Without a fence a restore
  cannot be seen at all, so the server refuses to start with an error naming
  `CALENDAR_FEED_FENCE_PATH` and `docs/self-hosted.md → Calendar Feed Restore Fence`, and writes
  nothing. Take the `ovumcy_fence` volume from the updated compose file for your stack. To start
  without it anyway, set `CALENDAR_FEED_FENCE_OPTOUT=true`: feeds are disarmed on every start and
  the log warns, but a restore on that instance can revive sessions and webhook endpoints.
