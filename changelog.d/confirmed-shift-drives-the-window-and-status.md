### Fixed

- **A confirmed thermal shift now moves the fertile window and the fertility status with it, on
  every surface.** When basal body temperature is tracked and the recorded readings show a sustained
  shift, that shift says the ovulation has already happened — nothing more. Ovumcy already moved the
  ovulation DAY onto the day inferred from those readings; the window around it and the "fertile
  today / not fertile today" status stayed on the projection the shift had superseded. An owner
  whose shift landed earlier than the model expected therefore read a confirmed ovulation day
  beside a fertile window that ended days later, and a "fertile" status for a day the same
  temperatures had already placed behind them — on the dashboard header and ring, on the month
  grid, on the statistics page and in the JSON overview, each of which had converted a different
  half of the answer.

  All of them now read one resolver: the confirmed day, the six-day window ending on it (clamped to
  the recorded cycle start, exactly as the projected window is), and the status computed over that
  window. Past the third elevated day the status is "not fertile", which is the only thing the
  method asserts. The projected next period is deliberately left alone — it stays a projection and
  is not recomputed from a confirmed ovulation.

  A confirmed shift also no longer needs the model to agree that a cycle can ovulate at all: an
  account whose recorded history is too short for the model to place an ovulation used to see its
  own temperature signal silently dropped from the calendar and the overview, and now sees the day
  its readings name. Where predictions are withheld altogether — unpredictable-cycle mode, a
  pregnancy pause, an overdue cycle, or before the first completed cycle — every surface stays as
  silent as before: a recorded observation never becomes a way around that.
