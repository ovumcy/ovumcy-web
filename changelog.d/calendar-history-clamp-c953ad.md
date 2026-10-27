### Fixed

- **The calendar reaches your whole stored history.** Its earliest month was the month of account
  creation minus three years, so older entries — imported or back-filled, and still served by the
  export, the API and the day panel — sat in months the calendar silently clamped away, along with
  the `?day=` that pointed at them. The lower bound is now the older of that window and your
  earliest stored entry (never before 1900-01, the oldest day a write accepts), and a disabled
  "Prev" says the calendar starts there. A month link that cannot be read (`?month=abc`) now opens
  the calendar with a notice saying so and keeps `?day=`, instead of redirecting without a word.
