### Fixed

- **Keyboard focus no longer falls to the top of the page after an in-page action.** Saving a day in
  the calendar editor, the late-cycle quick links, the goal quick switch and marking a cycle start (on
  the dashboard and in the calendar) used to leave focus on the page body, so a keyboard or
  screen-reader user had to start over from the top. Focus now stays on the Save button, moves to the
  section a late-cycle link names, or returns to the goal chip or cycle-start button after the page
  reloads. Removing a pregnancy-test result and revoking the calendar feed already kept focus, and
  now use the same shared helper.
