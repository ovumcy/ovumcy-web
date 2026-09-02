### Added

- **`ovumcy users set-email --id <id> <email>` — a repair for an account that cannot sign in and
  cannot be named by its address.** Sign-in normalization is strict about the stored email, and the
  first boot after upgrading rewrites the legacy decorated rows to their bare address. Two kinds of
  row it cannot rewrite are left standing, and both are locked out: two accounts on one mailbox
  (the older one keeps the address), and a value that reduces to no plain address at all. The
  runbook told operators to review such a leftover and "remove or re-home it" — but there was no
  re-home command, and the address form of `users delete` could not reach the row either: the
  stored string is refused as invalid input, while the bare address inside it belongs to the *other*
  account, so a delete typed that way erased the wrong account's entire health record.

  `set-email` addresses the account by the id `users list` prints. The new address is validated
  under the same rule a sign-in input is normalized under, refused if another account already
  answers to it, and written under a compare-and-set on the value the operator was shown, so a row
  that changed in between is reported rather than overwritten. The health record is untouched — the
  whole point of having a repair that is not a deletion — and every session of that account is
  revoked in the same write, because the address is the login identity. `users delete` gains the
  same `--id` form, whose confirmation quotes the exact stored address, id and role. The
  self-hosted runbook now walks the non-destructive path and says how to verify it.
