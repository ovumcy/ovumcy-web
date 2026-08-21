### Security

- **The recovery-code reveal now expires on the server, not just in the browser.** The sealed
  `ovumcy_recovery_code` cookie carried no expiry of its own, so its twenty-minute lifetime lived
  only in the `Set-Cookie` `Expires` attribute — a hint the client is free to ignore. A browser
  that kept the sealed value could present it back on the owner's own session and be shown the
  recovery code again, indefinitely, until the code was regenerated or `SECRET_KEY` changed. The
  payload now carries the expiry it is honored until, and the reveal page verifies that against
  the clock before it displays anything.

  A cookie minted before this change carries no expiry and is **refused**: an absent bound is
  invalid input, not permission to display. The refusal costs the display only — the page already
  required an authenticated session, so the owner keeps their sign-in, lands on the same continue
  path they reach when the cookie is absent, and can regenerate the code from Settings. A cookie
  refused for either reason is retracted in the same response.
