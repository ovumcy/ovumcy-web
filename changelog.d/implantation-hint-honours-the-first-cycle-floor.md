### Fixed

- **The implantation-bleeding hint waits for a completed cycle.** Marking an early bleed as a new
  cycle start used to offer the "this may be implantation bleeding" caution even for an account
  with a single recorded cycle start, where the ovulation it counts the days from is the configured
  cycle length rolled forward rather than anything observed. The hint now follows the same
  first-cycle floor the fertile-day save message, the calendar grid, the calendar feed, the webhook
  reminder and the dashboard banner already follow, and reappears once one cycle has completed.
