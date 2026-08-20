none

Test-only change with no user-visible effect: the register enumeration-parity
regression compared the two response bodies by length, so `0 == 0` on a pair of
empty 303 redirects passed whatever the branches actually returned. It now
compares them byte for byte, requires both to be explicitly empty, and drives
the same endpoint once more with `Accept: application/json` so the branch that
does carry a payload is decoded and pinned field by field. A duplicate-email
answer of `next_step=register_welcomf` — the same width as `register_welcome`,
which is exactly what a length comparison cannot see — now fails the guard.
