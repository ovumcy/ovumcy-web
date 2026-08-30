### Fixed

- **Docs now cover restoring a backup that predates a `clear-data` or account-deletion request.**
  `docs/self-hosted.md` documented the analogous calendar-feed resurrection case but never mentioned
  erasure at all: neither operation leaves a tombstone in the database, so a restore from a
  pre-erasure backup silently undoes the erasure with no signal that it happened. Added step 8 to
  Post-Restore Verification and a new `docs/gdpr.md → Backup Restore and Erasure` section telling
  operators to track erasure requests outside the database and re-apply any that postdate a
  restored backup. Documentation only, no code changed.
