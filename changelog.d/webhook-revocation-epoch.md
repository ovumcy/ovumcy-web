### Security

- **Turning a webhook off now also stops the reminder pass that is already running.**
  The pass reads every owner's notification settings once, at the start of its run, and sends each
  reminder some time later. Everything an owner can do to narrow or revoke it — switching delivery
  off, replacing the endpoint, removing it, shortening how far ahead reminders arrive, clearing all
  their data — changed only settings the running pass had already read past, so a pass in that
  window could still POST a period or ovulation
  reminder to the endpoint the owner had just been told was gone. The per-reminder watermark could
  not catch it: a settings save deliberately leaves the watermarks alone, and a clear-data wipe
  clears them, which put the pass back on its "nothing sent yet" path and let it send.

  Each of those writes now advances a per-owner revocation counter in the same statement that
  performs the write, and the claim the pass takes immediately before every request pins the counter
  its own snapshot carried. A pass holding settings the owner has revoked loses that claim and
  delivers nothing. The clear-data case advances the counter rather than resetting it, because a
  reset would have handed the stale pass its own value back.

  One limit is worth stating plainly, because it cannot be closed from here: a request that has
  already left cannot be recalled. If the pass had sent the POST at the moment the setting changed,
  that one request completes. What is guaranteed is that no new request begins under settings the
  owner has revoked. `docs/notifications.md` says the same thing where an operator will meet it.

  Schema: adds `users.webhook_config_version` (migration 038, both engines).
