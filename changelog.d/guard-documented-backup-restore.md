### Fixed

- **The Postgres restore in `docs/self-hosted.md` no longer promises an export that the procedure
  cannot produce, and its "changed nothing" case now says what it does change.** Running the
  documented commands settled both. A dump taken after the documented restore is never
  byte-identical to the one that was replayed: `pg_dump` 17.6+ wraps every dump in a fresh random
  `\restrict` token, and dropping and recreating `public` leaves a schema `initdb` did not create,
  so every later dump carries an `ALTER SCHEMA public OWNER` / `COMMENT ON SCHEMA public` /
  `REVOKE USAGE ON SCHEMA public` block the earlier one did not. Neither is data, and an operator
  who verified a restore with a byte-for-byte `diff` — which the old wording invited — would have
  read a good restore as a failed one. The runbook now names both differences and says to compare
  the `COPY` blocks and `setval` lines instead. The failure row of the same section is corrected
  too: replaying a dump into a database that still holds its schema restores no row, but `setval`
  is the one statement that still succeeds, so every sequence is rewound to the backup's value and
  the next insert lands on an id that is already taken.

### Internal

- **The backup and restore runbook is now executed, not just written.** Both documented procedures —
  the Postgres one (`pg_dump`, then a schema drop and a `psql -v ON_ERROR_STOP=1` replay) and the
  SQLite named-volume one (`tar czf` through Alpine, then `docker volume rm`/`create` and `tar xzf`)
  — existed only as prose: nothing in the repository ran a single one of those commands, so an
  incident would have been the first time either ran end to end. `scripts/backuprestoredoc` reads
  them out of `docs/self-hosted.md` and runs them verbatim against ephemeral resources, substituting
  only what would otherwise reach a real deployment: the compose transport becomes a direct
  `docker exec` against a throwaway container, and the data volume name becomes a throwaway volume.
  That second substitution is safety, not convenience — the documented restore begins by deleting
  the volume — so an executed command is refused outright if either literal survived it. The two
  `docker compose` lifecycle lines are asserted present and then skipped: the guard never starts the
  application, it writes the volume and reads it back itself.

  Each half seeds a generation, backs it up, deliberately drifts the database — a row written, a row
  changed, a row deleted — restores, and only then compares, so a restore that moved nothing cannot
  pass; both then read the owner and the day logs back through the repositories. The Postgres half
  closes with the runbook's own failure row as a counterfactual. The SQLite half writes its fixture
  with the connection held open, which puts the rows in `ovumcy.db-wal` rather than in the database
  file, and so checks the runbook's promise that the whole-volume archive captures `ovumcy.db`, its
  `-wal` and its `-shm` together — an archive missing the sidecars restores the clean, empty instance
  the Post-Restore Verification section warns about, and nothing checked that before.

  CI's relevance filter learns the one consequence: a diff that touches only the runbook now forces
  the Go lanes, which would otherwise be cleared as documentation — 10 of the 22 commits that touched
  it since June carried nothing else. The browser lanes are deliberately not forced with them: the
  guard does not ride there.
