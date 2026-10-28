### Fixed

- **The period-off question keeps following a day save when its feedback cannot be read.** Un-marking
  a period day asks first when the un-tick would move the cycle start or clear the auto-filled
  days after it. A save whose follow-up feedback failed to load left the open form with the question
  it was rendered with, even when the save itself changed what that question should say; the
  question is now recomputed from the saved day in that case.

### Changed

- The period-off question now travels with a stable locale key (`X-Ovumcy-Period-Off-Confirm-Key`
  beside the question header, `data-period-off-confirm-key` beside the toggle's question), and the
  component that renders it is parsed once at startup instead of being taken from an arbitrary page
  template.
