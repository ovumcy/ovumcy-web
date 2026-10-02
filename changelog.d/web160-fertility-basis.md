### Changed

- **`GET /api/v1/stats/overview` reports today's fertility status as a statement about an estimate,
  and says what it was read against.** `current_fertility` answers `fertile`,
  `outside_estimated_window` or `unknown`. A day outside the window is reported as outside an
  estimated window, never as a day on which conception is not possible: an estimated window often
  misses the real fertile days, regular cycles included, so `outside_estimated_window` is not a safe
  day and not a method of contraception — after a confirmed thermal shift too, where it only says
  that today lies outside the window derived from that shift.

  Two fields sit beside it. `fertility_basis` names the window the status was read against —
  `projection` for the one rolled forward from the cycle history, `confirmed` for the one derived
  from a shift the owner's own temperatures confirm — and is `null` exactly when the status is
  `unknown`. `cycle_data_stale` reports the verdict the dashboard and the statistics page show as
  their out-of-date notice: the running cycle has passed the account's reference cycle length. While
  it is true the endpoint answers `unknown` for both the phase and the fertility status, as both
  pages do; the projected dates are still reported unless a suppression signal withholds them.

- **In irregular-cycle mode the current cycle's fertile window covers the whole ovulation range.**
  An account with irregular-cycle mode on and three or more completed cycles is shown a range of
  possible ovulation days, from its shortest recent cycle to its longest, but the fertile window and
  the fertility status were built from the six days around a single median estimate — so the status
  called days "outside the window" that the same dashboard named as possible ovulation days. The
  current cycle's window now runs from five days before the shortest cycle's ovulation to the
  longest cycle's ovulation, on the dashboard, the statistics page, the calendar and the JSON
  overview; the cycles the calendar projects after it keep the median window. The published
  ovulation date stays the median estimate, so on the days between it and the window's end no phase
  is named — the ovulation may still be ahead of the owner. The window can run past the projected
  next period; the calendar marks those days as both.
