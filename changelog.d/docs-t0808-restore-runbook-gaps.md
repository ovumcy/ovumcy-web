### Fixed

- **The self-hosted backup runbook no longer calls a live whole-volume archive safe.** It stated
  that the archive captures `ovumcy.db`, `ovumcy.db-wal` and `ovumcy.db-shm` together, and asked for
  the app to be stopped only when an operator copies the files individually. Capturing all three is
  necessary and, against a running instance, not sufficient: `tar` reads them one after another, and
  a checkpoint landing between the read of the main database file and the read of the WAL writes the
  WAL into the main file *after* that file was read, then empties the WAL *before* it is read. The
  archive comes back carrying all three files and missing a commit that was in the database before
  the backup began, with nothing about the run reporting a problem. The runbook now asks for the app
  to be stopped before the data volume is archived — or for the archive to be taken from an atomic
  snapshot of the volume — and says so where an operator meets the archive command as well as in the
  backup contract.

- **Post-Restore Verification now sends an operator back to the calendar feed.** A restore returns
  the feed columns to their state at backup time, so a subscription an owner revoked or rotated
  *after* that backup was taken is armed again at its old subscribe URL. This is a different trigger
  from a `SECRET_KEY` rotation, which disarms feeds instead, and the runbook's rotation section
  covers only that one. `docs/gdpr.md` had recorded the restore case — confirmed in a live restore
  drill — and the file an operator actually follows through a restore never pointed at it. It is now
  step 7 of the checklist.

### Internal

- Both corrections are held by a test rather than by the next reader. `internal/db` captures one
  database twice, live with a checkpoint forced into the window between the main-file read and the
  sidecar reads, and again with the connection closed: the live capture restores the day written
  before the previous checkpoint and loses the one written after it, the stopped capture restores
  every day, and stopping is the only difference between the two. `scripts/backuprestoredoc`, which
  already executes the runbook's commands, now also holds both ends of the new cross-reference — the
  link in the checklist and the heading it resolves to — so neither can go missing on its own.
