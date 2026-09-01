none

Test-only fix. The onboarding step-1 picker spec asserted today's grid cell AFTER
pressing the "yesterday" shortcut. Selecting a date moves the grid to that date's
month, and the grid renders one month with no neighbouring days — so on the first
of a month "yesterday" is the previous month and today's cell leaves the DOM. The
spec passed on 30 or 31 days of every month and failed on the first: green for
months, then red on 2026-09-01 against a `main` whose last run, six hours earlier,
had been green with the same code.

The cell assertions now run before the shortcut, and the shortcut half gains an
assertion that the grid followed the selection into the previous month — the
behaviour that made the old ordering fail is now pinned rather than merely
avoided. No product code changes.
