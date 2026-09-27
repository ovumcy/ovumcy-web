### Fixed

- **The API reference now gives the real lifetime of a remembered sign-in: 30 days.**
  `docs/openapi.yaml` described `remember_me` on `POST /api/v1/sessions` as a cookie with a
  ~7-day `Expires`; the server has always set 30 days. The description now also says that without
  `remember_me` the cookie has no `Expires` or `Max-Age` but the session inside it still ends after
  7 days, lists the cookie attributes (`HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` under
  `COOKIE_SECURE`), and says a sign-in that goes through the 2FA challenge keeps the choice. A guard
  test reads both lifetimes from the spec and checks them against the cookie and the token the
  server issues. The server's behaviour is unchanged.
