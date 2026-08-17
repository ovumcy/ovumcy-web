### Fixed

- **A day note in a non-Latin script is no longer cut in half on save.** The notes field advertises a
  2000-character limit, but the server measured that number in bytes: 2000 typed Cyrillic characters
  are 4000 bytes, so roughly half the note was dropped on save, and 2000 emoji were dropped to 500 —
  with no warning and no error, the shortened text simply came back on reload. The limit is now
  counted in characters on both sides, so everything the field accepts is stored whole. The limit
  itself is unchanged at 2000 characters.
