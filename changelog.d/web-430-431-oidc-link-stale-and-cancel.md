### Security

- **Cancel on the "link this sign-in?" page now actually declines the identity.** The button was a
  plain link back to Settings, so the identity just declined stayed parked for the rest of its
  five-minute window: opening the page again offered the same confirm button, one mis-click away
  from linking a sign-in the owner had rejected. Cancel now clears the pending link, and the page
  has nothing left to confirm. A plain visit to Settings from another tab still leaves a pending
  link alone.

- **Confirming a link from before a password change no longer signs this device out.** A link
  parked before the password was changed in another tab is stale, but confirming it was answered
  as if the sessions had been revoked under the link: this device was signed out and a revocation
  was logged for a link that was never written. It now answers like any other expired link —
  nothing is linked, the device stays signed in, and the owner is asked to start the link again.
