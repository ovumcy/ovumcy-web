none

Test-only hardening for three WEB-15 (P14 Data integrity) gaps, no user-visible
effect. Account erasure's child-delete rollback test now seeds the full
schema-derived set of user-scoped tables and asserts every surviving table's
rows are restored after a failed delete, not just `users`. The SQLite
concurrent-day-write regression now fails on any unexpected non-BUSY error and
asserts a successful write per disjoint day slot plus the final row count, so a
run where every write errors can no longer read as a pass. A new day-upsert
regression reads the raw stored row (not the export payload or the rendered
page) to prove every trackable field — flow, mood, sex activity, BBT, cervical
mucus, pregnancy test, cycle factors, notes, symptom IDs — actually clears to
its stored zero value when the owner submits an untracked/preserved-off save.
