### Fixed

- **Ovulation and fertile-window estimates now wait for three completed cycles.** A regular
  (non-irregular) account with one or two completed cycles used to get an ovulation date and a
  fertile window on the dashboard ribbon, the calendar, `GET /api/v1/stats/overview`, the calendar
  feed and the "ovulation soon" webhook reminder, built from one or two observed cycle lengths.
  Below three completed cycles that half of the projection is now withheld everywhere, the way it
  already was for irregular accounts and for an account with no completed cycle; the next-period
  estimate stays. The ribbon no longer labels days "Ovulation" or "Fertile" in this state, and the
  calendar feed and the webhook send no ovulation event until the third cycle completes. The stats
  API names the state with a new suppression reason, `awaiting_more_cycles`, added to the
  `reasons` enum of the overview response; a client that branches on the reasons should accept it.
  The floor withholds projections only: past completed cycles keep their inferred shading under
  "show historical phases". An ovulation day the owner's own temperatures confirmed is still
  shown. An account trying to conceive sees a line in the dashboard header saying the fertile
  window appears after the first three completed cycles, in place of the withheld ovulation
  estimate.
