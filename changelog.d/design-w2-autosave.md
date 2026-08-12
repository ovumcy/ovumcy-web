### Changed

- **The dashboard journal saves itself, and a save can be taken back.** The
  "Update entry" button is gone: the day form has one save model, the auto-save
  that was already running behind it. The row under the form stays silent while
  there is nothing to report, says "Saved" once the server has answered, and
  offers a single step back beside it — Undo restores the state the day held
  before that save and sends it the same way, or clears the day again when the
  save was the first one on an empty day. Nothing is remembered outside the open
  page. A save that does not land now reuses the day editor's neutral notice
  with its Try again control instead of a bare state change, on the autosave and
  the HTMX paths alike, and everything typed stays on screen. The calendar day
  editor keeps its explicit Save.
