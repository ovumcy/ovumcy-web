### Fixed

- **A refused symptom action on Settings now says so.** Editing, hiding or restoring a symptom
  that is not in your list (a stale page, a built-in symptom) re-rendered the card with no message;
  the refusal now appears at the top of the symptoms card and receives focus. It reads the same
  whether the symptom never existed or belongs to another account on the instance.
- **Clear-data re-authentication explains the lockout.** After too many wrong passwords the status
  beside "Clear all data" read "Request failed. Please try again."; it now shows the same
  too-many-requests message as account deletion.
