none

Test-only. `OpenDatabase` applies the embedded migrations of the driver it
opened, so SQLite and Postgres run two separate bodies of SQL, and until now
nothing compared what those bodies produce: the existing
`TestEmbeddedMigrationSetsMatchAcrossDialects` compares the two trees by
migration version and file name only. A migration creating a table or a column
in one tree and forgetting it in the other kept every version and every file
name aligned, left that test green, and still shipped two different schemas —
which then narrows anything derived from the live schema, such as the
account-erasure completeness sweep that finds its user-scoped tables by their
`user_id` column. A new `internal/db` test now migrates both engines through
`OpenDatabase` and compares the resulting table sets and, per shared table, the
column-name sets. Column types are deliberately not compared (integer/bigint,
text/varchar and the boolean representations differ legitimately). Being a
comparison of two schemas, the test skips as a whole when docker is absent;
it runs wherever the Postgres container does. No production code or migration
changed.
