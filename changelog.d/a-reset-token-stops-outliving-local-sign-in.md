### Security

- **A password-reset link no longer outlives local sign-in.** Turning local public authentication
  off (`OIDC_LOGIN_MODE=oidc_only`) stopped the recovery flow from *starting* — `/forgot-password`
  refused — but nothing stopped a reset that had already started. A token minted the minute before
  the switch still redeemed: it rewrote the password, turned the account's local sign-in back on,
  and handed out a session on an instance the operator had just closed to local sign-in. The redeem
  now follows the same gate as the flow that mints it, and `/reset-password` follows both. A
  **forced** reset is untouched: an account carrying "must change password" reaches that page
  through SSO, which is precisely what still works in `oidc_only`, so refusing it would strand the
  owner with no way to clear the flag.

- **Two pages that spend a one-time value now require a real navigation.** `/register/welcome`
  exchanges the registration hand-off for a session, and `/settings/calendar-feed` reveals the
  subscribe URL exactly once; both are ordinary page loads, and a browser sends cookies with a page
  load started from someone else's site. Neither could be made to hand anything to that site — the
  single-use marks behind them already saw to that — but both could be made to *burn* a value the
  owner had not seen yet: a link that quietly finished her registration, or spent her one look at
  the subscribe URL. Both pages now check who started the request and answer anything but a
  first-party navigation the way they answer a stale one, leaving the value unspent for the owner's
  own visit. A first-party app fetching those pages the documented way still gets them, and a browser
  too old to say who started a request is served as before.

- **A recovery reset no longer remembers a device nobody chose.** Signing in remembers a browser
  for 30 days only when the sign-in form's *Remember me* box is ticked, but two paths that carry no
  such box — finishing a password recovery and picking up a fresh registration — asked to be
  remembered on the owner's behalf. Both now keep their session for the browser session, the same
  default an unticked box gets. The same choice was also being thrown away in the other direction:
  changing the password, turning two-factor on or off, or clearing the data re-issues the session,
  and that re-issue always came back session-scoped — so a browser the owner had asked Ovumcy to
  remember quietly stopped being remembered, on exactly the screens where she was least likely to
  notice. The re-issue now carries the original choice forward, and ticking the box on the sign-in
  form works as it always did.
