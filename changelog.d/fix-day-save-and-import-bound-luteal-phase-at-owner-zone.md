### Fixed

- **A day save, delete, cycle-start mark, or restore near midnight could refine the stored luteal
  phase from the wrong day.** The derived `luteal_phase` estimate is recomputed after every one of
  those writes, bounded at "today" so the derivation never reads a cycle start that has not
  happened yet. That bound was read from the request's timezone (the browser's header/cookie)
  instead of the account's own stored timezone — the same bound the boot-time recompute and the
  two egress passes already use. An account near a date line, or simply logging in a timezone the
  request happened to carry a stale value for, could see its derived luteal phase refine (or fail
  to refine) a day earlier or later than the boot pass would have computed for the same data — a
  disagreement between the two that only the *next* write corrected.

  Every writer of the derived value now resolves the account's own stored timezone first, falling
  back to the request's zone only when none is stored yet, matching the resolver the webhook and
  `.ics` feed passes already share. Nothing changes for an account whose stored timezone matches
  the browser's.
