### Fixed

- **Onboarding step 1 now asks for the last period start once, through a month calendar.** The
  screen carried two mechanisms for the same value — a 60-button day grid that was clipped
  mid-row on a phone, with the "Next" button overlapping the cut-off row and reading as
  disabled, plus a separate day/month/year field beside it. Both are replaced by a single
  compact picker: "Today" and "Yesterday" shortcuts, a month grid whose cells are real buttons
  labelled with the full date, month names and weekday headers in the interface language, the
  owner's week-start honoured, and every date after today rendered inert. Nothing scrolls
  inside the picker, so at 390px the whole month fits above "Next" instead of behind it. The
  submitted field, its name and its ISO format are unchanged, and the server's range validation
  is untouched.
