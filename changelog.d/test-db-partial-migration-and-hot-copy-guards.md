none

Test-only guards for two persistence properties that held with nothing pinning
them. The first drives the migration runner with a three-statement migration
whose middle statement the engine rejects, and requires that the failure leave
neither the first statement's table nor a `schema_migrations` row claiming the
migration applied — a row without the schema change would make every later boot
skip that migration for good. The neighbouring failure cases stop earlier: they
fail an injected catalog read, or are refused by the runner's own post-condition
after every statement has already run. The second pins the backup hazard the
self-hosting runbook warns about: copying `ovumcy.db` alone while the instance
runs restores a database that opens cleanly, reports no error, and is missing
every day still resident in `ovumcy.db-wal`. No production code changed.
