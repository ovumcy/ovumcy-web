### Fixed

- **A database that stops answering during startup now fails the start instead of hanging it.**
  Ovumcy runs three repair and sentinel passes when it starts, after the migrations and before it
  begins serving: the calendar-feed key-rotation check, the one-shot auth-email repair, and the
  one-shot luteal-phase recompute. Each waited on storage with no time limit. A database that
  accepts the call and then never answers — a SQLite file held by another process, a Postgres
  endpoint that connects and stalls — left the server alive and silent for as long as that lasted:
  no listener, no error, and a container healthcheck with nothing to report but "starting". An
  operator can read a refusal; there is nothing to read in a hang.

  Each pass now runs under a five-minute budget. The number is deliberately far above what any of
  them costs — the heaviest reads every owner's day logs once, uncontended, because nothing is
  serving yet — so reaching it means storage is stuck rather than slow. What happens then is
  unchanged and still differs per pass: the feed-rotation check and the email repair stop the boot,
  because a feed left armed or an account left unable to sign in is worse than an instance that
  refuses to start, while the luteal-phase recompute logs the failure, leaves its marker unwritten
  and lets the server start, since it maintains a cache with a safe fallback.

### Internal

- **The boot sequence is swept for unbounded passes rather than trusted.** A guard reads the window
  in `main()` between the repositories being built and the dependencies being wired — which is what
  makes something a boot pass: the database is open, the migrations have applied, no listener exists
  — and holds every call it finds there to taking its context from the shared budget. A fourth pass
  added to that window is covered without anyone remembering to extend a list. The guard fails if
  the window yields fewer calls than the passes known to run there, so a sweep that reached nothing
  cannot report success, and a companion test runs both checks against synthetic sources to prove
  they can separate a bounded pass from one reaching for `context.Background()`.
