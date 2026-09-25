none

Wording only: a comment in `e2e/a11y-wcag-audit.spec.ts` and the changelog fragment for the
calendar-contrast de-flake now state the month-view constraint in place (the calendar grid renders
only the viewed month padded to whole weeks, so a cell is asserted in its own month's view)
instead of pointing at a document that is not part of this repository. No behavior changes.
