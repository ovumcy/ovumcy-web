### Security

- **Turning provider logout off now takes effect at the next sign-out.** When an instance signs an
  owner in over SSO with `OIDC_LOGOUT_MODE=provider` (or `auto`), Ovumcy stores the material that
  sign-out would need — the provider's end-session address and the session's `id_token_hint` — for
  up to seven days. That record was being treated as the decision itself: after the operator
  switched the mode to `local`, or turned OIDC off, an owner who had signed in before the switch was
  still sent to the provider's end-session page for the rest of the record's life, exercising a mode
  the operator had turned off. The mode is now read when the sign-out happens, so such a session
  signs out locally at `/login` from the next request, and the record it would have used is
  discarded with the session rather than left to expire. Nothing was exposed by the old behaviour —
  the end-session address was already re-checked against the issuer configured now, and the return
  address always came from the current configuration — and a `provider` or `auto` instance is
  unaffected.
