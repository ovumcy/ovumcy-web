### Security

- **A sign-out or credential change made while the restore-fence file briefly failed can no longer
  be undone by restoring a backup.** A sign-out, password change or reset, TOTP toggle, OIDC link or
  unlink or recovery-code rotation that met a failing fence-file write recorded the change in the
  database alone. If a backup taken before it was restored before the next start, the restored
  database agreed with the untouched fence file, and the signed-out cookie or replaced credential
  worked again. These writes now retry the fence-file write, and when the fence volume stays
  broken they are refused with nothing changed and can be retried; a refused sign-out keeps the
  session so it can be signed out again. Clear-data and account deletion still go through, as
  before, after the same retries.
