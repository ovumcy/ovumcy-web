### Security

- **Breaking (API shape): a CSRF token does not outlive the authentication state it was minted under.**
  Every sign-in (password, the 2FA challenge, the OIDC callback, a password reset, the
  post-registration pickup) and the sign-out retire the token the request carried: the cookie is
  expired and, where the middleware saw the request, the token leaves its storage. A JSON client
  that kept a token across `POST /api/v1/sessions` or `DELETE /api/v1/sessions/current` is now
  refused with `403 csrf_refused` and fetches a fresh one from any page. Browser forms are
  unaffected: the redirect that follows a sign-in or sign-out mints the next token.
- An owner who has not finished onboarding can sign out: `POST /logout` is no longer redirected
  back to `/onboarding` with the session alive, and the onboarding page carries its own sign-out
  control. The sign-out confirmation now says that it also ends the account's sessions on every
  other device, which it always did.
