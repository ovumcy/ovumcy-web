### Changed

- **Settings sections fold on a phone.** Each card is a disclosure below 640px and opens on a
  tap; the section index still jumps straight into a folded one. On a wider screen, and with
  no JavaScript, the page is unchanged. A section holding unsaved edits never folds. Measured
  at 390px: the page went from 8856 px of scrolling to 1460.
- **The additional-tracking toggles no longer repeat themselves.** Five helper lines restated
  their own toggle's label and then repeated the section's own promise that turning a field
  off never removes what is already logged. The toggle that shades past cycles keeps its
  hint: those phases are derived, and its label does not say from what.

### Internal

- Layout is now measured in every shipped language, not only in English, so a translation
  longer than its English source can no longer push a control off a 390px screen unnoticed.
