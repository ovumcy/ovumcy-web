### Fixed

- **After the usual cycle length passes, the calendar's start window and the dashboard cycle
  ribbon match the dashboard header.** At that point the dashboard header, reminders and the
  calendar feed move the next-period start window to the following cycle, but the calendar grid
  still shaded the window for the cycle that had just ended, and the ribbon still drew it. The grid
  now shades the window the header names. The ribbon shows only the current cycle, so it draws no
  start window once the header has moved to the next one.
