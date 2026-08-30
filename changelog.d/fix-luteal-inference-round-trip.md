### Fixed

- **A personalized ovulation estimate no longer lands a day before the day your own logs pointed
  at.** Once enough basal-temperature or cervical-mucus entries let the app learn your own luteal
  phase instead of assuming the 14-day model default, that length was measured one day too long: it
  counted the ovulation day itself, which belongs to the phase before it. The prediction built from
  it then sat one day early — the ovulation date and both edges of the fertile window alike — on the
  dashboard, the calendar, the stats cycle ribbon, the API, the webhook reminders and the `.ics`
  feed. An ovulation observed
  on cycle day 15 came back as a prediction for cycle day 14. Learning and predicting are now two
  directions of one arithmetic, so an ovulation observed on a cycle day is predicted on that same
  cycle day when the next cycle runs the same length. Accounts still on the 14-day default, and
  every recorded observation, are unchanged. One side effect is worth naming: the 10-to-20-day
  plausibility window a cycle must fall inside to count is now measured the corrected way too, so a
  cycle that sat exactly on its lower edge no longer counts. An account whose signal only just
  qualified can fall back to the 14-day default rather than shift by a day.
