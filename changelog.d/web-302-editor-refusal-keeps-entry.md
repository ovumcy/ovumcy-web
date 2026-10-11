### Fixed

- **A refused control in the calendar's day editor keeps what was typed.** When another account had
  signed in since the calendar page was opened, the cycle-start or delete control inside the day
  editor was refused — correctly — but the refusal notice replaced the whole editor, unsaved entry
  included. The notice now leads the open editor and the entry stays on screen; only an editor that
  had not opened yet gives way to the notice.
