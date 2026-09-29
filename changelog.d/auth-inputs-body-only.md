### Security

- **Breaking (API shape): credentials, sign-in flags and CSRF tokens are read from the request body only, never from the URL.**
  The sign-in `remember_me` flag, the 2FA challenge code, the 2FA enrollment code, the 2FA disable
  password, the CSRF token and the email echoed back on a failed forgot-password form were read
  through a lookup that searches the query string before the body. A link such as
  `/api/v1/sessions?remember_me=1` therefore turned a session-scoped sign-in into a 30-day one, and
  a code, password or token in a URL was accepted and left in logs, history and referrers. All of
  them now come from the JSON or form body, and a value in the query string is ignored:
  `POST /api/v1/sessions?remember_me=1` with a false or absent flag issues a browser-session cookie,
  a request whose CSRF token is only in the URL is refused with `403`, and a 2FA code or password
  only in the URL is treated as missing. The CSRF token belongs in the `X-CSRF-Token` header or the
  `csrf_token` form field, the only two the API reference declares. Browser forms are unaffected.
  `PUT` and `DELETE /api/v1/users/current/2fa` now also accept the JSON body the other
  password-protected settings calls take (`{"password", "code"}` and `{"password"}`), and the API
  reference declares it. Those inputs are read from JSON, urlencoded and multipart bodies only: a
  body of any other type (XML, CBOR, MessagePack, a vendor `+json`) is refused rather than decoded,
  and so is a password-protected settings call whose body the decoder could not read whole.
