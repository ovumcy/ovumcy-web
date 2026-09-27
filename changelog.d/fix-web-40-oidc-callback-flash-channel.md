### Security

- **A cross-site hit on the OIDC sign-in return can no longer erase a pending settings or auth
  message.** `/auth/oidc/callback` cannot require a CSRF token — a provider has to be able to post
  back to it from another site — so an unrelated or malformed request arriving there used to
  overwrite whatever flash message a same-origin page redirect had just queued (a "settings saved"
  banner, a forced sign-out notice), and the owner's next page load showed the callback's refusal
  instead, or nothing at all. Refusals from that route, from `/auth/oidc/start`, and from the two
  other requests reachable without a CSRF token now travel on their own channel: a same-origin
  page's pending message is shown first, and a genuine provider refusal still reaches the owner
  whenever nothing else was pending.
