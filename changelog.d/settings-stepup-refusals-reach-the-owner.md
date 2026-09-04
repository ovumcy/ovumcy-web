### Fixed

- **A refused step-up now says so on the settings page.** Linking an OIDC identity, enrolling a
  local password, or confirming a data wipe or account deletion at the identity provider returned
  the owner to an unchanged settings page whenever the provider re-authentication was refused —
  stale, belonging to another session, reported as an error by the provider, or an identity already
  linked elsewhere. Nothing was shown, so a refusal looked exactly like a success. The refusal is
  now displayed, in all six languages.
