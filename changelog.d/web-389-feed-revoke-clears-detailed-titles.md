### Security

- **Revoking a calendar feed now also clears its detailed-titles choice.** Revoke, clear-data, a
  recovery-code regeneration, a password reset and the boot-time disarms removed the subscribe
  link but left the "name the estimate type" opt-in on the account. A release older than v2.0.0
  mints a link without touching that setting, so after a downgrade, a new link there and an
  upgrade back, the new link sent "Period (estimate)" and "Ovulation (estimate)" titles the owner
  never chose for it. Every disarm now resets the choice to neutral titles. A link an older
  release rotates in place stops working after the upgrade back instead, and the owner generates
  a new one; `docs/self-hosted.md → Downgrade Caveats` describes both cases.
