none

Test-only guards for two persistence properties that held with nothing pinning
them. The first drives the migration runner with a four-statement migration the
engine rejects part-way through, and requires that the failure leave nothing
behind: not the table an earlier statement created, not the health field an
earlier statement overwrote, not a `schema_migrations` row claiming the
migration applied, and not one row fewer in the rest of that ledger. A row
recorded without the schema change would make every later boot skip that
migration for good; rows lost from the rest of the ledger would make the next
boot re-apply the whole set against a schema that already has it. The
neighbouring failure cases stop earlier: they fail an injected catalog read, or
are refused by the runner's own post-condition after every statement has already
run. The second pins the backup hazard the self-hosting runbook warns about:
copying `ovumcy.db` alone while the instance runs restores a database that opens
cleanly, reports no error, and is missing every day still resident in
`ovumcy.db-wal`. No production code changed.
