### Added

- The database now records when a webhook delivery was actually accepted, and which key
  regime a calendar-feed link was issued under. Both are written only when the event
  happens — a delivery mark appears after the endpoint answers, never when the send is
  attempted — and both start empty on upgrade rather than being guessed from existing
  data. Nothing displays them yet.
