### Security

- **An email address two accounts share is refused on every web sign-in path instead of being
  resolved to one of them.** A database whose unique index on the normalized email was dropped or
  restored away outside the app can hold two accounts on one mailbox (the migration itself refuses
  to build the index over duplicates). Password sign-in, recovery reset and SSO sign-in resolved
  such an address to whichever row the query returned first, so the account a sign-in reached
  depended on storage order, not on who was signing in. Such an address is now refused: sign-in and
  recovery answer exactly as for an unregistered address, with the same response and the same
  password-hashing work, and SSO answers as it does for an existing account the identity is not
  linked to, linking and creating nothing. Operator commands already refused it and name the
  matching account ids, so the duplicate can be resolved by id.
