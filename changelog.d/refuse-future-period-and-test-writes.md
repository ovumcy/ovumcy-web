### Fixed

- **A period or a pregnancy-test result can no longer be recorded on a day that has not happened.**
  A day save — the dashboard and calendar forms, with or without JavaScript, and `PUT` or `PATCH
  /api/v1/days/{date}` — accepted a period or a test result on any future date, and the calendar
  then showed it as recorded, while marking a cycle start more than two days ahead was already
  refused. Both are observations, so a save that turns the period on or records a test result
  past today plus two days (in your timezone) is now refused with the same message as a cycle
  start that far ahead. Saves that record neither — a note, a symptom, or clearing a period or a
  test — still go through on any date, and entries already stored ahead are kept as they are and
  stay editable.
