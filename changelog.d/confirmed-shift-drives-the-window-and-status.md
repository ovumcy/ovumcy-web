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

  The cycle phase moved with them. Ovumcy estimates how long a period lasts from your own average,
  and used to call every day inside that estimate "menstrual" even when the ovulation day published
  right beside it fell on the same day or earlier — one screen answering two ways about one date.
  That estimate is now cut short at the published ovulation day, so a day past a confirmed shift
  reads as luteal rather than as an estimated period still running. A day you logged bleeding on is
  untouched: what you recorded outranks every estimate, and always did. The month grid follows the
  same rule — the expected-period shading now stops before the ovulation day rather than sitting on
  the same cell as the ovulation marker and the fertile shading. So does the dashboard ring, which
  shortens its period band instead of disappearing, which is what a shift confirmed early in a cycle
  used to make it do, to the owners tracking their temperatures most closely.

  A confirmed shift also no longer needs the model to agree that a cycle can ovulate at all: an
  account whose recorded history is too short for the model to place an ovulation used to see its
  own temperature signal silently dropped from the calendar, the overview and the dashboard — whose
  hero ring read the same withheld claim independently of the calendar's fix and stayed hidden — and
  the hero ring is visible again. The reminder banner reads a different gate and stays as it was:
  it only ever announces a day still ahead, and a confirmed day is by construction several days
  behind today, so it was never the day this fix addresses. Where predictions are withheld
  altogether — unpredictable-cycle mode, a pregnancy pause, an overdue cycle, or before the first
  completed cycle — the silence is now more complete than before, not merely unchanged: the cycle
  phase label used to keep naming the withheld ovulation day even after every other field went
  quiet, so a suppressed account could still read "ovulation" today, or "follicular"/"luteal" on
  either side of it, off the phase alone. The phase is now recomputed from the same fields the rest
  of the response withholds, so it can say "menstrual" during a logged or projected period, or
  "unknown" otherwise, but never point at the day suppression exists to hide. The dashboard's cycle
  ribbon widget read the day through a second, separate path and stayed just as loud: its phase
  label and its phase-card breakdown followed the same suppressed ovulation day the rest of the
  response had already gone quiet on. The ribbon itself — the axis, the day cells, the "today"
  marker — still draws; only the parts that named the day now don't. A recorded observation still
  never becomes a way around any of this.

  Going quiet is not the same as going blank, and the ribbon now says which one it is. The days
  whose phase depends on the withheld ovulation day carry a status of their own — "fertile details
  held back" — in their own colour, instead of the empty track that means the projection has run
  out. Mid-cycle, the difference is the difference between "there is more to show later" and
  "tracking has stopped". The dashboard's phase line says the same words as the cells beneath it
  rather than a second phrase for the same silence. And an account still waiting for its first
  completed cycle — the tier with nothing behind the fertile half but the cycle length from setup —
  is now told so in a sentence under the ribbon, which is where every other reason for a quiet
  prediction already says its name.
