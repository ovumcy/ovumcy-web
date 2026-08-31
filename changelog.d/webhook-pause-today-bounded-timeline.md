### Fixed

- **A cycle start recorded for a future day no longer ends a pregnancy pause early — and no longer
  ends it on the webhook alone.** A positive pregnancy test pauses predictions everywhere until a
  cycle start is logged after it, and a cycle start may be entered up to two days ahead. Those two
  rules met in the wrong place: the pause is resolved over whatever set of day logs the surface
  handed the shared derivation, and only some surfaces bounded that set at today. The dashboard,
  the calendar and the `.ics` feed fetch a range that ends today, so they went on showing the pause.
  The webhook notification pass loads the whole stored history, so tomorrow's start lifted the pause
  for it alone: an account could read "paused" on every screen while an outbound `period-soon`
  notification left for their endpoint saying the predictions had resumed.

  The bound now sits in the derivation every surface shares rather than in each caller's fetch, so
  the pause — and the cycle statistics around it — are resolved over one timeline that ends on the
  account's own today, in the account's own timezone. A day logged ahead of today still stores,
  still displays, and still counts from the day it arrives; it simply no longer decides anything
  before then. Nothing changes for an account whose recorded days are all in the past.
