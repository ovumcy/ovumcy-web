### Fixed

- **Two reminder passes running at the same time no longer send the same reminder twice.** The
  notify pass decides what is due from the snapshot of accounts it read at the start, and it used to
  record "sent" only after the request came back. Two passes that both read before either recorded —
  the built-in scheduler and an `ovumcy notify` run started by hand or by cron, which is a separate
  process and therefore beyond anything the app can lock — each saw an unsent reminder and each
  delivered it, so the owner's endpoint received one health-derived reminder twice. The pass now
  claims a reminder before the request goes out, by a conditional write only one pass can win; the
  pass that loses skips it, and the one that wins delivers. A failed delivery hands the claim back,
  so the reminder is still retried on the next pass exactly as before, and `--dry-run` continues to
  make no request and to write nothing at all. One trade comes with it, now written down in the
  notifications guide: a pass killed outright mid-request — a reboot, an OOM kill, an interrupted
  `ovumcy notify` — skips that cycle's reminder instead of retrying it, because a missed reminder is
  a smaller harm than a duplicate one about health data.
