### Fixed

- **Linking or unlinking an OIDC identity, or clearing your data, no longer reports itself as
  failed when only the follow-up session re-issue could not be carried out.** If the change
  committed but a concurrent revocation or a session fault kept this device from being
  re-issued a session past it, the owner used to see "failed to create session" or "failed to
  clear data" — a message that is false, since the change already went through. Both cases now
  say so and ask the owner to sign in again.
- **A settings change refused after this device was signed out now lands on the sign-in page
  with its message.** Linking an identity, unlinking one, clearing data (by password or by
  provider step-up) and setting a local password each clear the session cookie when a
  concurrent revocation wins or the session cannot be re-issued. Their answer used to redirect
  to the settings page, which bounced to sign-in and dropped the message, or — for a
  password-confirmed clear-data sent from a plain form — rendered a raw JSON error. A browser is
  now redirected to `/login` with the message, and an HTMX request gets `HX-Redirect: /login`
  instead of an inline banner on a page that no longer has a session.

### Changed

- **JSON API:** when an identity unlink (`DELETE /api/v1/users/current/oidc/identities/:id`) or a
  data wipe (`POST /api/v1/users/current/data-wipe`) has committed but its session could not be
  re-issued, the response is now `401` with category `unauthorized` instead of `500`. The error
  key is `identity change applied sign in again` for the unlink and `data cleared sign in again`
  for the wipe, replacing `failed to create session`; both mean the change took effect and the
  client must sign in again. A refusal that changed nothing keeps its previous status and key.
