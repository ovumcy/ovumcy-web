### Security

- **A start refused for a fence it cannot write no longer costs every owner their password on the
  next start.** Without `CALENDAR_FEED_FENCE_OPTOUT`, a start whose calendar-feed restore fence can
  be read but not written — a bind mount the container user cannot write to, a full disk — was
  refused correctly, but only after it had already marked the database as having run without a
  fence. Once the operator fixed the mount, the next start read that mark as a possible restore and
  signed every account out, removed every webhook endpoint and revoked every local password. The
  refused start now writes nothing, so the start after the fix is an ordinary first start.

- **`ovumcy reset-password --full` now also removes the account's webhook endpoint.** Someone who
  took over an account could point its reminders at an endpoint of their own, and the reset that
  recovers the account left that endpoint in place, so `ovumcy notify` kept posting period and
  ovulation reminders to it. `--full` now withdraws the endpoint in the same write that resets the
  password: the URL is cleared, delivery is switched off, and a reminder pass already running stops
  before its next request. The owner adds the webhook again from Settings. The plain
  `reset-password` keeps it, as before.
