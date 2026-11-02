### Security

- **A first start that fails to record its restore-fence marker no longer costs every owner their
  password on the next start.** A first start of v2.0 wrote the calendar-feed restore fence file and
  then had its database write fail — a locked or full database, a transient Postgres error. The next
  start read the lone file marker as a restored backup and signed every account out, removed every
  webhook endpoint and revoked every local password, on an instance that had never served. The
  first start now marks its file write as unfinished until the database holds the marker, so the
  next start simply completes the first start.
