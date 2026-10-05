### Fixed

- **Saving a Positive or Remove pregnancy-test result on the dashboard now updates the page in
  place.** The field still read "No result recorded" with no Remove action after a Positive
  result, the status header and the period-reminder banner kept their old estimate until a reload,
  and Remove dropped keyboard focus to the page body. The field now follows the click, the status
  header is refreshed from the dashboard once the result is saved (only the blocks that depend on
  the result are replaced, so the goal switch beside them keeps working and an open goal menu stays
  open), and focus moves to the first result control when Remove hides its own button. Undo puts
  the field back with the value it restores, a refresh keeps keyboard focus on the same link
  inside the refreshed block, and the status line announces the change to screen readers.
