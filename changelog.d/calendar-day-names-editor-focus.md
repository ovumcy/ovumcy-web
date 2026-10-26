### Fixed

- **Calendar days tell a screen reader what they show.** A day's accessible name was its date
  alone, so a recorded period, a predicted one, the fertile window, an estimated ovulation, a logged
  entry and today were all silent. Each day now reads its date followed by the legend's words for
  what the cell draws — a fertile day as an estimate, as the ovulation mark already is — today is
  marked as the current date, and the selected day reads as pressed.
- **Opening a calendar day moves to it.** Choosing a day, or Edit, Cancel or Delete inside the day
  panel, now moves keyboard focus to the panel's heading and brings the panel up below the header
  when it sits under the month or out of view — on a phone a tap looked like it did nothing, and
  after Edit focus fell back to the top of the page. The month is a single Tab stop navigated with the
  arrow keys instead of one stop per day, and Escape in the day panel returns to the selected day.
