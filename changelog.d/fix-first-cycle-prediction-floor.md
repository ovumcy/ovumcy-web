### Fixed

- **No fertile window, peak band or ovulation day before the first cycle is complete — on any
  surface.** Until one cycle has been observed, that window is nothing but the onboarding
  cycle-length slider projected forward, and the dashboard header has always withheld it. Four other
  surfaces did not: the calendar grid painted a fertile window, a peak band and an ovulation day; the
  `.ics` feed pushed three ovulation events into subscribed calendar clients, where they outlive any
  correction made in the app; the webhook pass sent an ovulation reminder to the configured endpoint;
  and the dashboard's own reminder banner counted down to a date the header beside it was declining
  to name. All four now stay quiet until a cycle closes, and resume with the first completed one. The
  next-period estimate is unchanged everywhere — it is anchored on a day that was actually recorded
  and carries its own estimate qualifier — and so are recorded observations such as logged period
  days and the BBT signal.
- **One stats page, one cycle history.** A cycle start marked uncertain was honoured by the insights
  ribbon and ignored by the phase cards on the same page, so the page reported one merged 56-day
  cycle in one panel and two 28-day cycles in the other — and the merged length then fed the
  cycle-factor comparison, which labelled that fabricated cycle "longer" and attributed the logged
  factors to it. Both halves now read the same detector, the one that honours an explicit start and
  withholds a cluster whose only explicit start is uncertain.
- **The stats current-phase card states its phase where it can be read.** On a stale cycle the card
  withheld the phase in words only; it now carries the unknown phase and fertility status in the
  same attributes the rest of the page exposes, so the withholding is visible to assistive
  technology and pinnable by a test.

### Internal

- The three suppression signals every projected surface shares (`PredictionsSuppressed`) and the
  first-cycle fertility floor on top of them (`FertilityProjectionSuppressed`) live in one place
  each, instead of being written out once per surface — which is how the floor came to be missing
  from four of them without a failing test. Surfaces built from the dashboard cycle context read the
  decision that context already resolved rather than recombining the signals for themselves, so a
  future disjunct reaches all of them at once.
- The stats service doubles now record the owner id of all four owner-scoped reads (ranged logs, all
  logs, frequency calculation, symptom catalogue) and can serve different data per owner. A
  two-owner stats render pins each seam: every one of the four could previously have been re-pointed
  at a constant owner with the whole stats selection still green.
