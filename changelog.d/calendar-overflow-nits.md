### Fixed

- **A long note or symptom name no longer widens the calendar page.** A note with a long unbroken
  run, such as a pasted link, or a 40-character symptom name used to stretch the whole page at phone
  width, month grid included. Both now wrap inside the day panel.
- **Calendar nits.** The day panel heading names the year when the day is not in the current year.
  The previous- and next-month links keep the open day; Today still opens today. A day save fetches
  the month grid once instead of twice, and marking a cycle start no longer starts a grid fetch that
  the page reload then discards. Phones now show a compact today mark in the today cell. A
  calendar link that opens a day for editing, such as the dashboard's link to log yesterday, now
  scrolls a day panel stacked under the month into view; opening another day from the grid drops
  that link's edit mode from the address.
