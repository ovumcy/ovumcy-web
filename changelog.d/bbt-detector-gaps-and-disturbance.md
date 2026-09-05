### Changed

- **The temperature-shift documentation now says what the detector is and what it is not.**
  `docs/cycle-prediction.md` lists every surface the one detector feeds and spells out where the
  applied "3-over-6" rule departs from the full symptothermal protocol on purpose: no second
  indicator cross-checks a shift, a slow rise is never rescued by a fourth day, a day tagged
  `illness` or `sleep_disruption` is dropped whole, and the six values behind the coverline are
  recordings rather than calendar days, so skipped mornings lengthen the window backwards. The
  confirmed day stays an estimate inferred from a signal, and no accuracy interval is promised for
  it.

### Internal

- Characterisation tests pin the window's behaviour across skipped and disturbed days, and pin
  that a rise climbing in steps below the margin is not confirmed.
