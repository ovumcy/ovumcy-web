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
- **An owner tracking to conceive whose first cycle runs long is told when the fertile window
  arrives, instead of nothing.** Before the first completed cycle the status line carries one
  dateless sentence — "your fertile window appears after your first completed cycle" — in place of
  an ovulation estimate. Once that first cycle passed the reference length the sentence disappeared
  too and the slot was simply empty, at the point the owner has the most reason to wonder. Pausing
  a projection withholds a date; this line names none, so it now stays for as long as the first
  cycle is open. It still goes when predictions are off altogether — unpredictable-cycle mode or a
  pregnancy pause — where promising a future window would contradict the rest of the page.
