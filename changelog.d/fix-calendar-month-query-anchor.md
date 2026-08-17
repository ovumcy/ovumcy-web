### Fixed

- **The calendar no longer opens the wrong month in time zones whose clock jump
  lands on the first of a month.** In zones west of UTC where daylight saving
  starts at 00:00 on a first-of-month (America/Asuncion 2023-10-01,
  America/Havana 2012-04-01), local midnight does not exist that day. The month
  the page anchors on was parsed and built directly in the request zone, so the
  missing midnight resolved one calendar day backward: asking for that month
  rendered the whole grid, the heading and the month value for the *previous*
  month instead. The month links inherited it — "previous" skipped a month, and
  "next" pointed back at the page already shown, so the affected month could not
  be reached at all — and the same construction shifted the lower navigation
  bound a month past the account's own history. Month anchors are now stepped as
  calendar dates and resolved through the single day-construction point, so the
  requested month is the month that renders. Zones and months without such a
  transition, including UTC, behave exactly as before.
