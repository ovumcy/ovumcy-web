### Fixed

Resetting a password, regenerating the recovery code, and enrolling a local
password after an OIDC step-up each rotate the recovery code and revoke every
session. They used to commit the new code first and only then issue the new
session and stage the code's one-time reveal, so a failure in between left the
account with a code nobody had been shown and the previous one gone. The
session and the reveal are now sealed inside the same database transaction,
before it commits; if either cannot be sealed, nothing is written — the
password, the previous code, existing sessions and the calendar feed stay as
they were, and the action can simply be retried. The session re-issued after
the rotation now carries the version read back from the database rather than
one computed from the account as loaded before the write.
