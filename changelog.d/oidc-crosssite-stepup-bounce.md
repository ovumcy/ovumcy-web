### Fixed

- **A re-authentication at a provider on another site now finishes the action it was asked for.**
  Linking an SSO identity, setting a first local password, clearing data and deleting the account
  all send the owner to the provider and complete when it sends her back. When that provider lives
  on a different site and returns the answer as a form post, the browser withholds the session
  cookie on the way back — that is what `SameSite=Lax` is for — so the app could not tell whose
  re-authentication had just arrived and refused all four with "that re-authentication does not
  match the account signed in here". The return now lands on a short same-site page that carries the
  answer forward: the app reads the session there exactly as it always did, and no cookie was made
  reachable from another site to achieve it. That page's own hand-off is single-use, expires in a
  minute, is scoped to one address, and that address turns away any request another site starts. A
  refusal comes back the same way: when the provider declines, or sends back something that does not
  answer the request that was made, the owner arrives on the settings page and is told why — on every
  browser, rather than only on those that treat a redirect begun elsewhere as still being her own
  visit. A provider on the same site is unaffected.

- **An abandoned re-authentication no longer blocks the next sign-in.** Starting one of those
  settings actions and then leaving the provider without finishing left a note in the browser that
  said "a re-authentication is in progress". For the next ten minutes that note answered every
  attempt to sign in with SSO — the sign-in came back from the provider, was mistaken for the
  abandoned action, and was turned away with nothing shown on the page. Starting a sign-in now
  discards the abandoned action, and so does signing out.

### Security

- **A stray hit on the sign-in return address can no longer cancel a sign-in or a re-authentication
  in progress.** The return address cleared its one-time cookie before checking whether the request
  belonged to that flow at all, so any load of that address — a stale tab, a prefetch, a form
  another site submits — discarded what the owner was in the middle of and left her to start
  again. The cookie is now spent only for a return that answers the flow it was minted for; its own
  one-time nature and expiry are unchanged.
