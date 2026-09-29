### Security

- **The server no longer starts on a database that has lost its normalized-email unique index.**
  `idx_users_email_normalized` is the only thing that stops two accounts from sharing one email
  address; sign-in already refused an address two accounts share, but that locks both accounts out
  after the fact rather than preventing the duplicate. The index can only go missing when it is
  dropped outside the application or lost in a restore, and it never comes back on its own. After the
  migrations apply, the server now reads the index's definition from the database (`sqlite_master` on
  SQLite, `pg_indexes` and `pg_index` on Postgres) and refuses to start if it is missing, is not the
  unique index on `lower(trim(email))` that migration 002 builds, or, on Postgres, is marked invalid
  (as a failed `CREATE INDEX CONCURRENTLY` or `REINDEX` leaves it). Nothing is repaired at startup.
- **Operators:** the refusal names the index and the fix. With the server stopped, run
  `DELETE FROM schema_migrations WHERE version = '002'` (after `DROP INDEX idx_users_email_normalized`
  when the refusal says the index has a different definition or is invalid) and start again:
  migration 002 is re-applied and re-creates the index, or refuses and names the address if two
  accounts already share one.
