### Fixed

- **The dashboard no longer states a fertility verdict for an account whose predictions are
  suppressed.** The header's fertility gate carried one of the four suppression signals — the
  zero-completed-cycle floor — so an owner in unpredictable-cycle mode, on a pregnancy pause, or
  past their own reference cycle length still read "Fertile window" on the status line, and the
  header still declared "fertile" or "not fertile" as its fertility state. In unpredictable-cycle
  mode that line sat next to the notice saying predictions are off. All four signals now decide it,
  through the same gate the calendar grid, the `.ics` feed and the reminder banner already use: in
  those states the status line shows no fertile-window item and the header declares no fertility
  state at all, while the cycle day, the phase and recorded observations are untouched.
- **The journal grid stops publishing that verdict too.** The grid below the status header carried
  the raw fertility classification with no gate on it whatsoever, so it stayed readable to scripts
  and styling in every suppressed state, including before the first completed cycle. It now reads
  the same single verdict the header does.
- **An overdue cycle no longer names a next-period date to an owner with irregular cycles and
  little history.** Withholding the estimate past reference + 7 days reached everyone except the
  one group tracking irregular cycles with fewer than three completed: they kept seeing a projected
  date captioned "needs more cycles" — a date rolled a whole cycle forward from a period that
  should already have started. That owner now gets the same "estimate paused" notice everyone else
  past that point gets. Owners inside their reference length still see the estimate with its
  caption.
