### Fixed

- **The JSON answer of `POST /api/v1/users/current/webhook` now reports the settings as saved.**
  `webhook_enabled`, `notify_period` and `notify_ovulation` were copied from the request, so a save
  with `webhook_remove_url: true` (which forces delivery off) could still answer
  `"webhook_enabled": true` while the stored row said otherwise. The three flags are now read back
  after the write. The set of fields is unchanged, and the endpoint URL is still never returned.
