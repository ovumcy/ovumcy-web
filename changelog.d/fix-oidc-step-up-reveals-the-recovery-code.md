### Fixed

- **Enrolling a local password on an OIDC-only account now shows the recovery code it mints,
  instead of dropping the owner on the dashboard with nothing.** The enrollment finishes at the
  provider's callback, which redirected to `/recovery-code`. That page claims the account's
  one-time reveal, and the claim is guarded on Fetch Metadata so only a same-origin initiator may
  spend it. `Sec-Fetch-Site` describes the whole redirect chain, and the callback is a cross-site
  POST the provider makes, so the redirect arrived still labelled off-origin: the guard refused, the
  reveal was skipped, and the owner landed on `/dashboard` — the freshly minted code shown to
  nobody, with no route back to it. She could still regenerate one from Settings, and nothing was
  disclosed, but the code the flow exists to hand over was lost every time.

  The callback now hands the browser a same-origin document that performs the hop, the same
  interstitial the outbound provider hop already uses. The navigation into the reveal is then
  started by this origin, so the label the guard reads is true rather than excused, and a page on
  another site still cannot produce one — the guard itself is unchanged.

  The regression reached `main` because the browser lane that walks this flow is opt-in
  (`E2E_OIDC_PROVIDER=local` with `OIDC_LOGIN_MODE=oidc_only`) and skipped in default CI.
