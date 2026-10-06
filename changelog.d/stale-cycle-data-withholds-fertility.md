### Fixed

- **Ovulation and fertile-window estimates are now withheld once the running cycle is past its usual
  length.** From the first day past the account's reference cycle length — the day the pages
  already said "Cycle data may be outdated" — the dashboard, the calendar grid,
  `GET /api/v1/stats/overview`, the calendar feed and the "ovulation soon" webhook reminder used to
  keep naming an ovulation date and drawing a fertile window for a next cycle whose start was never
  logged. That half of the projection is now withheld on every surface until a new period is
  logged; the next-period estimate stays, until the existing overdue cut-off a week later. The
  stats API names the state with a new suppression reason, `cycle_data_stale`, added to the
  `reasons` enum of the overview response; a client that branches on the reasons should accept it.
  An ovulation day the owner's own temperatures confirmed is still shown. The plain-words late-cycle
  notice now appears from that same first day instead of a week later, and it is shown on the
  calendar and stats pages as well as on the dashboard. On the stats page it shares one card with
  the existing note about recent cycles longer than 45 days instead of standing beside it, and the
  `cycle_data_stale` flag and reason now always change on the same day.
