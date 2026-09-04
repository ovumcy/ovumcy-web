### Fixed

- **A refused step-up now says so on the settings page.** Linking an OIDC identity, enrolling a
  local password, or confirming a data wipe or account deletion at the identity provider returned
  the owner to an unchanged settings page whenever the provider re-authentication was refused —
  stale, belonging to another session, reported as an error by the provider, or an identity already
  linked elsewhere. Nothing was shown, so a refusal looked exactly like a success. The refusal is
  now displayed, in all six languages. The same return now also covers the step after the
  re-authentication: if enrolling the local password fails to be saved, or this device's session
  cannot be re-issued afterwards, the owner gets the settings page with an explanation instead of a
  raw error object.
- **Turning on two-factor authentication no longer leaves the enrollment cookie behind when the
  sign-in cookie cannot be refreshed.** Confirming the first code enables 2FA and re-issues this
  device's sign-in cookie. If that last step fails, the browser kept the short-lived cookie holding
  the authenticator seed until it expired on its own; it is now cleared along with the refusal, as
  it already was when the enrollment succeeded.
- **Turning two-factor authentication on or off now confirms itself where the owner can see it.**
  The success side of the same defect the refusals had: both answers were attached to the settings
  confirmation carrier and then sent to the two-factor page, which does not read it — and attached
  as a raw message name, which the page that does read it resolves to nothing. So the confirmation
  was invisible twice over, and the unread one rode along to whichever page consumed it next. Both
  now return to the settings page, which shows the confirmation together with the account's new
  two-factor state.
- **Switching the tracking mode from the dashboard no longer plants a stray "saved" message on the
  next settings page.** The quick switch returns to the dashboard, which shows the new mode itself
  and reads no confirmation carrier; the confirmation written for it was invisible on arrival and
  then appeared on the settings page the owner opened next, as a save they had not just made.
