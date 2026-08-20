none

Test-only. `OpenDatabase` applies the embedded migrations of the driver it
opened, so SQLite and Postgres run two separate bodies of SQL, and until now
nothing compared what those bodies produce: the existing
`TestEmbeddedMigrationSetsMatchAcrossDialects` compares the two trees by
migration version and file name only. A migration creating a table in one tree
and forgetting it in the other — or declaring the same column `INTEGER` here
and `TEXT` there — kept every version and every file name aligned, left that
test green, and still shipped two different schemas. That narrows anything
derived from the live schema, such as the account-erasure completeness sweep
that finds its user-scoped tables by their `user_id` column, and it hands the
application a string on one engine where the other gives it a number. A new
`internal/db` test now migrates both engines through `OpenDatabase` and
compares the resulting table sets, the column-name sets per shared table, and
each shared column's coarse type family (temporal, boolean, text, binary,
float, integer). Differences inside a family are ignored, because they are
legitimate: `integer`/`bigint`, `text`/`varchar`, `real`/`double precision`.
Being a comparison of two schemas, the test skips as a whole when docker is
absent; it runs wherever the Postgres container does. No production code or
migration changed.
