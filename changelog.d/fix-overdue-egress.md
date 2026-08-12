### Fixed

- **Reminders and calendar events stopped inventing dates for a cycle that had not started.** Once a
  cycle runs past the account's reference length by more than a week, the projection can only roll a
  whole cycle forward at a time — and three surfaces kept following it. The webhook reminder
  announced a "period soon" for a date nothing in the account supported, and because its
  already-sent marker is the predicted date itself, every roll re-armed a new one, repeating about
  once per cycle for as long as the cycle stayed open. The `.ics` feed pushed up to three cycles of
  invented period and ovulation events into subscribed calendar clients, where they outlive any
  correction made in the app. The calendar grid shaded a predicted period in the past — on days that
  came and went with no period logged — and phantom fertile windows ahead of it. All three now stay
  quiet for an overdue cycle, exactly as the dashboard already does; logged days, their markers and
  the calendar subscription itself are untouched. When the new cycle start is finally logged, the
  next reminder fires once, on time.
