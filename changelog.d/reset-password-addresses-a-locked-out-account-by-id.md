### Added

- **`ovumcy reset-password` gains the same `--id <id>` addressing `users delete` and `users set-email`
  already have.** The operator repair for an account locked out under a legacy stored email — two
  accounts on one mailbox, or a value that no longer parses to a plain address — reached everything
  except its password: `set-email` could re-home such a row and `delete` could remove it, but
  `reset-password` still took only a bare address, which either refused outright for the locked-out
  row or, on a shared mailbox, quietly resolved to the *other* account that already holds the bare
  address — resetting a stranger's password instead of the one the operator meant.

  `reset-password <email>|--id <id>` mirrors the sibling commands' exact convention: mutually
  exclusive, exactly one required, same flag spelling and the same `users list`-pointing error
  wording. As a second line of defense, the email form now also refuses — naming every matching id
  — rather than silently resolving to whichever row a lookup happens to return first, if an address
  is ever ambiguous. The same refusal was lifted into the shared lookup `users delete <email>` and
  `webhook show|set <email>` already resolve accounts through, so all three inherit it uniformly
  instead of only the one command this fix started on.
