### Internal

- **`docs/notifications.md` no longer claims no third party is involved in any channel.** The
  same document tells owners to subscribe the feed from Google Calendar, Apple Calendar or
  Thunderbird, and a webhook URL is validated only as absolute http/https, so an owner can and
  does point either at a hosted service. The wording now says no third-party service is
  *required or built in*, and names the owner's own choice as the thing that decides who
  receives the reminder data.
- **`docs/self-hosted.md` describes the rate limiter the code actually has.** It told operators
  the app keys per-IP limits on the leftmost, spoofable `X-Forwarded-For` entry; the edge
  limiters key on the rightmost untrusted hop and are not spoofable. The recommendation
  (a header the proxy overwrites) is unchanged; what is corrected is the mechanism, the
  residual (`c.IP()` feeding the secondary per-client auth buckets), and the claim that the
  app's default is `X-Real-IP` when it is `X-Forwarded-For`.
- **The self-hosted OIDC section no longer restates the account contract lossily.** Its
  "falls back to a verified email match" omitted the mandatory password-plus-TOTP confirmation
  — the exact takeover the gate exists to stop. The section is now the operator's decisions
  plus a pointer to `docs/oidc.md`, which owns the contract.
- **`docs/oidc.md` keeps the operator flow and points at the invariant's home.** The pending
  cookie's TTL, the retry behaviour and the already-claimed refusal had three prose homes; a
  TTL change had to be made in all of them. `docs/security/oidc-and-sessions.md` owns them now.
- **`/readyz`'s documented body is the JSON the handler writes** (`{"status":"ok"}` /
  `{"status":"unavailable"}`), not "a fixed one-word body" an operator might match on.
