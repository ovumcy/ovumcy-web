### Fixed

- **A re-authentication at a provider on another site now finishes the action it was asked for.**
  Linking an SSO identity, setting a first local password, clearing data and deleting the account
  all send the owner to the provider and complete when it sends her back. When that provider lives
  on a different site and returns the answer as a form post, the browser withholds the session
  cookie on the way back — that is what `SameSite=Lax` is for — so the app could not tell whose
  re-authentication had just arrived and refused all four with "that re-authentication does not
  match the account signed in here". The return now lands on a short same-site hop that carries the
  answer forward: the app reads the session there exactly as it always did, and no cookie was made
  reachable from another site to achieve it. The hop's own hand-off is single-use, expires in a
  minute, and is scoped to that one address. A provider on the same site is unaffected.

### Security

- **A stray hit on the sign-in return address can no longer cancel a sign-in or a re-authentication
  in progress.** The return address cleared its one-time cookie before checking whether the request
  carried anything valid, so any load of that address — a stale tab, a prefetch, a link from
  another site — discarded a flow the owner was in the middle of and left her to start again. The
  cookie is now spent only once its contents prove valid; its own one-time nature and expiry are
  unchanged.
