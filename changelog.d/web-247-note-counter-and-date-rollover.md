### Fixed

- **The note and symptom-name counters now count what the field limit counts.** The counter beside
  the day note (dashboard and calendar editor) and beside a custom symptom name counted characters
  as code points, while the browser's length limit counts UTF-16 code units, in which an emoji
  outside the Basic Multilingual Plane takes two. A note of emoji therefore stopped accepting input
  while its counter still read half full. Both counters now count in the limit's unit, so the
  counter reads full exactly where typing stops; plain text is counted as before.
- **A dashboard left open past midnight moves to the new day instead of saving into yesterday.**
  Every day URL on the dashboard is fixed when the page is rendered, so a journal edit made after
  the owner's local date had changed was saved to the previous day with nothing on screen saying
  so. The page now notices the date change — on a once-a-minute check, when it is shown again after
  being in the background or restored, and when a field is focused or typed into — and reloads onto the new day.
  The reload waits while anything is outstanding: an edit already typed, still saving, or refused
  and waiting for a retry stays on screen, is saved to the day the page shows, and the reload
  follows that save. Typed text is never discarded for it.
