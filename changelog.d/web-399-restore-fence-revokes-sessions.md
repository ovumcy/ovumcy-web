### Security

- **Restoring a backup no longer brings back sessions that were signed out, or webhook endpoints
  that were removed.** A sign-out everywhere, a password change or reset, an operator reset, an
  account deletion and a webhook removal are all recorded in columns a database restore rolls
  back, so restoring a backup taken before one of them made every sign-in cookie minted before
  the backup point valid again and sent reminders to the removed endpoint. The restore fence
  already disarmed calendar feeds on the first start after a restore; that same start now also
  signs every owner out and clears every stored webhook endpoint.

  **Operators: after a restore, every owner signs in again and re-enters their webhook URL** in
  Settings; their reminder choices are kept. A lost or recreated fence volume
  reads as a restore and costs the same. An instance running without the fence volume only
  disarms feeds on each start, so mount it — `docs/self-hosted.md → Calendar Feed Restore Fence`.
