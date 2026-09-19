### Security

- **The single sign-on page must now belong to the configured provider.** Ovumcy already refused
  a provider whose discovery document placed the token and key endpoints on another host, but it
  still followed the sign-in page address wherever that document pointed — so a tampered document
  could send the browser, with the sign-in request attached, to a look-alike page on a host the
  provider does not own. The sign-in page is now held to the same rule: same scheme, host and port
  as `OIDC_ISSUER_URL`, or single sign-on stays unavailable. A provider whose sign-in page lives on
  a separate host needs `OIDC_ISSUER_URL` pointed at an issuer whose endpoints share its origin.
  The provider sign-out address saved for a session is checked against the configured issuer again
  before the browser is sent there, so after the issuer changes, signing out of an older session is
  local only.

- **An OIDC address written in non-ASCII characters is refused.** A host such as the full-width
  `０.０.０.０` passed the "must name a remote host" check, yet HTTP clients and browsers convert
  it to `0.0.0.0` before connecting. Any host containing a non-ASCII character is now rejected; an
  internationalized domain keeps working in its ASCII `xn--` form.
