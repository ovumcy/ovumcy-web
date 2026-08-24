### Security

- **A refused sign-in or recovery reset now costs the same whether or not the address has an
  account — including accounts created before the password hashing cost was raised.** The
  equalizers that make an unknown address spend the same bcrypt work as a wrong password were
  pinned to the current cost, while the real comparison spends only the cost stored in the row. An
  account still holding a hash from before the raise was therefore refused about four times faster
  than an address with no account at all, and that gap told anyone measuring it which addresses are
  registered. A refusal now buys the missing work at the missing cost steps, derived from the
  configured cost and the stored one rather than from a fixed pair, so the totals match again.
  Recovery codes are the case that could not heal itself: their hashes are never re-minted on use,
  so a cost written years ago stays where it was.
- No stored hash is rewritten by this change and nothing about signing in changes for an owner: a
  successful sign-in still upgrades a stale password hash in place, as it has since the cost was
  raised.
