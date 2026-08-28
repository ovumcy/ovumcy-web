### Security

- **Signing out now retracts `ovumcy_tz`, the browser's timezone cookie, as it already did the
  language cookie.** Both are plaintext, neither is sealed or scoped to a session, and left
  behind on a shared or borrowed browser they tell the next visitor that this app is used here,
  in which language, and from which region — before anyone authenticates. Logout, account
  deletion and the other deliberate session ends now clear both; a session the server merely
  *refuses* still leaves them alone, which is the boundary that keeps a lapsed visitor's login
  page in their own language.

  The retraction needed its client half to mean anything: the timezone bootstrap script wrote
  the cookie synchronously on every page it loaded on, and every htmx request carried the
  `X-Ovumcy-Timezone` header the middleware re-issues the cookie from — so the login page the
  sign-out redirects to put the cookie straight back. The bootstrap script is now served only on
  a page rendered for a session, and the header rides only on those requests. Nothing changes
  for a signed-in owner: the cookie is written and sent exactly as before, and no anonymous
  surface renders a date to need it.

  Regressions: `TestLogoutRetractsTheTimezoneCookie` (with and without the request header),
  `TestSessionRejectionLeavesTheTimezoneCookieInPlace`, and
  `web/src/js/__tests__/timezone-cookie-signed-in-only.test.mjs`.
