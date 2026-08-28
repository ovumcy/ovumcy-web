### Internal

- **The security concern docs stop misdescribing the code they document.** Accuracy pass from the
  2026-08-28 documentation audit over three of the `docs/security/` concern files. `logging.md`:
  the audit-line example uses the route's real `PUT` method; the `health_data` target enumeration
  names all eleven values instead of seven; the `/lang` + interface-endpoint sentence no longer
  claims nothing is persisted — both persist `users.interface_language` (migration 034) for a
  signed-in owner, and the doc now records the audit exclusion as a declared decision about a
  presentation preference; the always-on diagnostics section counts six owner-naming lines, states
  that the webhook watermark is claimed before the send (so there is no post-success write to
  fail), and discloses the host-only webhook delivery skip/failure lines. `known-disclosures.md`:
  the registration timing equalization runs at bcrypt cost 12, not 10; the register probe's
  follow-up `GET /register/welcome` carries no limiter of its own — the probe is bounded
  transitively through the rate-limited POST that mints each single-use pickup; the
  calendar-feed-polling entry no longer claims every other human-facing health read is audited
  (in-app page reads are not audited at all); the clear-data description enumerates what the wipe
  resets — including the stored timezone — and what deliberately survives it (identity, language,
  TOTP, an OIDC link, onboarding), and the `requires_totp` accepted disclosure moves here from the
  cryptography doc, where it sat under Field-Level Encryption. `cryptography.md`: `ovumcy_csrf` is
  a persistent one-hour cookie refreshed per response, not a browser-session cookie; the
  total-secret-loss paragraph stops claiming an OIDC-only account loses self-service — the
  `(issuer, subject)` link is plaintext and the state cookies are minted under the current key, so
  a fresh provider sign-in still keeps access (a broken TOTP enrollment stays operator work: both
  2FA controls are password-gated) — and now also names the other casualties of a lost key (armed
  feed MACs, the feed key epoch, the in-memory limiter counters). `SECURITY.md`'s
  known-disclosures index gains the moved entry, and the single-document-era "see *Threat Model*
  below" pointer becomes a file link. No behaviour changes.
