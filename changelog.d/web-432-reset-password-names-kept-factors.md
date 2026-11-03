### Fixed

- **`ovumcy reset-password` now says what a plain reset leaves in place.** Without `--full` the
  command keeps the account's linked OIDC identities, two-factor sign-in, recovery code and webhook
  endpoint, and a linked identity can still sign in and replace the temporary password while the
  forced change is pending. The success message said none of this. It now names each of them that
  the account has, and points at `--full` for an account that may be compromised. An account deleted in the moment
  between the command's lookup and its write is now reported as not found, in the same words as an
  unknown account, instead of as a generic reset failure.

- **The compose files no longer understate what losing the fence volume costs.** Their comment
  still said it cost one round of re-generated subscribe URLs; since sessions, webhooks and local
  passwords joined the fence, losing it reads as a restore and signs every owner out, clears every
  webhook endpoint and revokes every local password, once. The self-hosting guide said the same in
  two places and is corrected too.
