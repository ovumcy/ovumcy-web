### Fixed

- **Linking or unlinking an OIDC identity, or clearing your data, no longer reports itself as
  failed when only the follow-up session re-issue could not be carried out.** If the change
  committed but a concurrent revocation or a session fault kept this device from being
  re-issued a session past it, the owner used to see "failed to create session" or "failed to
  clear data" — a message that is false, since the change already went through. Both cases now
  say so and ask the owner to sign in again, and land on the sign-in page rather than bouncing
  through an already-signed-out settings page that would have dropped the message entirely.
