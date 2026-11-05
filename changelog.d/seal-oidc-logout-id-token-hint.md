### Security

- **The stored OIDC sign-out hint is sealed.** To sign you out at your identity provider as well,
  the server keeps the ID token the provider issued for each OIDC session for up to seven days.
  It used to sit in the database in cleartext, so a database dump or backup carried a usable token
  for every recent OIDC session. It is now encrypted under the instance secret and bound to your
  account, and a value that does not open is treated as absent (WEB-489). Upgrading applies
  migration 044, which deletes the cleartext rows: an OIDC session that was open during the
  upgrade signs out locally once, without the provider hop. Downgrading past 044 needs a manual
  `DELETE FROM oidc_logout_states`, described in `docs/self-hosted.md → Downgrade Caveats`.
