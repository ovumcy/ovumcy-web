### Security

- **A cross-site hit on the OIDC sign-in return can no longer erase a pending settings or auth
  message.** `/auth/oidc/callback` cannot require a CSRF token — a provider has to be able to post
  back to it from another site — so an unrelated or malformed request arriving there used to
  overwrite whatever flash message a same-origin page redirect had just queued (a "settings saved"
  banner, a forced sign-out notice), and the owner's next page load showed the callback's refusal
  instead, or nothing at all. Refusals from that route, from every `/auth/oidc/*` path (including a
  sign-in rate limit), and from the other requests reachable without a CSRF token or a same-origin
  proof now travel on their own channel: a same-origin page's pending message is shown first, and a
  genuine provider refusal still reaches the owner whenever nothing else was pending.
- **Every rate limiter, not only the sign-in one, now answers a token-less refusal on the same
  channel.** Every login, registration, password-reset, logout and generic-API rate limiter runs
  ahead of CSRF checking, so any of them — not just the sign-in one the previous fragment named —
  could be tripped by a cross-site, token-less flood and used to plant a message on the same-origin
  flash slot; the password-reset limiter also echoed the request's own `email` field into that
  message. All of them now answer on the token-less channel described above, and the echoed email is
  dropped rather than carried across. The one visible change: a rate-limited password-reset
  submission no longer re-shows the email address the owner just typed on the redirect back to the
  form — she re-enters it once.
