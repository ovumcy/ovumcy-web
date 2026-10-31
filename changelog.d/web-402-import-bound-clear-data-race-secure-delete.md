### Security

- **Restoring a backup is held to the same date bound as saving a day.** A JSON restore could
  write a period or a pregnancy-test result for any future date, which then fed the predictions.
  A file whose new days record either more than two days ahead is now refused whole, and nothing
  of it is restored. Days already present are skipped as before.
- **Clear data on Postgres no longer keeps a day saved while it runs.** A day written by a request
  still in flight when the wipe started could survive it. The wipe now waits for such a write and
  erases what it left.
- **Erased data no longer lingers in the SQLite file.** Clear data and account deletion now
  overwrite the erased rows with zeros (`secure_delete`), so the deleted health records cannot be
  read back from the database file's free space.
