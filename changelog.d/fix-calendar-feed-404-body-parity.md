none

Test-only guard: the calendar-feed no-oracle regression now compares the four
bad-token responses against **each other** — status, headers and body bytes —
instead of checking each one separately for the absence of a calendar marker,
and pins the shape they share so they cannot drift together into a 404 that
explains itself. The 500 case asserts the app's generic `internal_error`
envelope with the injected storage error absent from the body. No user-visible
change — the route already answered identically in all four cases; nothing
could observe it, so a 404 carrying its cause and a 500 carrying the raw error
both stayed green.
