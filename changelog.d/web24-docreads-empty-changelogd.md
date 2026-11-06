### Internal

- **The document-reader CI guard no longer fails on a release commit.** Its check that `changelog.d` is still read as a directory looked for a fragment file under it, and release assembly leaves only `.gitkeep` there. It now checks the bare-name read itself and expands that one directory on its own, so it holds with or without fragments and still fails when either half of the derivation breaks.
