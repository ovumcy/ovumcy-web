### Fixed

- **An overdue cycle no longer gets a confident next-period window.** The projection rolls forward a
  whole cycle at a time, so it always produced a future date: on cycle day 45 with a 28-day
  reference the dashboard named the anchor plus 56 days as firmly as it names tomorrow, and the
  reminder banner counted down to it. Once a cycle runs past the reference length by more than a
  week — the same threshold that raises the late-cycle notice — no next-period window, ovulation
  estimate or reminder banner is shown at all; one line says the estimate returns with the next
  cycle start. The cycle day, the late-cycle notice and its actions stay exactly as they were.
