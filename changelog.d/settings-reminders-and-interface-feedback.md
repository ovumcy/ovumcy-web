### Fixed

- **Breaking (API shape): the reminder lead-days setting now refuses a value outside 0-14 instead of
  silently saving the nearest one.** An empty, non-numeric or out-of-range value is answered with
  its own message in the reminders form ("Enter a whole number of days from 0 to 14.") rather than
  the password sentence, and a JSON caller of `PATCH /api/v1/users/current/reminders` receives a
  400 where it used to receive a 200 with the clamped value. Saving the Interface card with the
  same language now confirms in place without reloading the page to the top; a language change
  reloads to the Interface card with its confirmation beside it, the confirmation is announced to
  screen readers and takes focus, and the theme control no longer pre-selects Light (the theme is
  still remembered in this browser only).
