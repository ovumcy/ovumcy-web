### Security

- **Restoring a backup no longer brings a revoked calendar feed back to life.** Revoking or
  rotating an `.ics` subscription cleared the feed columns and nothing else, so restoring a backup
  taken before that revocation returned them exactly as they were: the old subscribe URL served the
  owner's calendar again, under the same application secret, with nothing in the restored data
  recording that a revocation had ever happened. The behaviour was documented as a step the
  operator had to remember after every restore — re-check each owner's feed and revoke it again —
  and only the account itself can see or change its own feed, so an owner nobody told kept a live
  subscription they believed was gone.

  Ovumcy now settles it without the operator in the loop. A **restore fence** keeps a marker in two
  places, one inside the database and one in a file outside it, and advances both together on every
  change to the set of armed feeds. A restore rolls back only the copy inside the database, the two
  disagree, and the instance disarms every armed calendar feed on the boot that follows — before it
  accepts a request, so no poll can race it. Owners re-generate their subscribe URL from
  `Settings → Calendar feed`. This works on SQLite and on PostgreSQL alike, and does not depend on
  the application secret changing: the existing key-epoch sentinel covers a rotated `SECRET_KEY`
  and structurally could not cover this, because the epoch it compares is restored together with
  the feed columns it is meant to judge.

  **Operators: this needs one change to your deployment.** The fence file has to live somewhere your
  database backups do not capture, which is the whole mechanism. The bundled compose stacks now
  mount a separate `ovumcy_fence` volume for it, and the image points `CALENDAR_FEED_FENCE_PATH`
  there by default. Take that volume from the updated compose file for your stack. Never include it
  in a backup and never restore it beside the database — a fence restored together with the
  database agrees with it and detects nothing. Losing it is cheap by design: it holds no health data
  and no secrets, and the cost is one round of re-generated subscribe URLs.

  **Run the operator CLI where the server's fence is visible.** `ovumcy reset` and
  `ovumcy users delete` both remove calendar-feed access, and record that through the same
  fence. Through `docker compose exec` on the running container they already see it; from a
  host shell that does not, they still complete — an erasure never depends on a mount — but
  warn, and the server disarms every armed calendar feed on its next start.

  An instance with nowhere to keep the fence — the image pulled without the new volume, or the
  binary run outside compose with the variable unset — starts normally and keeps working, but
  disarms every armed calendar feed on each start and says so in its startup log, naming the
  variable to set. Without a fence a restored backup cannot be told apart from the database it
  replaced, so the feed fails closed rather than quietly returning to the old behaviour. Full
  contract: `docs/self-hosted.md → Calendar Feed Restore Fence`.
