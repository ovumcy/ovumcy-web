### Security

- **The provider sign-out record is now read and deleted per account, never by session alone.** An
  instance can host several independent owners, and the record kept so you can be signed out of
  your identity provider holds the token that provider issued for that session. It was looked up —
  and deleted — by the session identifier on its own, with nothing tying it to the account it
  belongs to. There is no known way for one owner to obtain another's session identifier, so this
  closes the gap rather than a demonstrated leak, but a boundary that holds only because the key is
  hard to guess is not a boundary. Every read and every delete now names the account as well, and a
  request that names none is refused instead of matching them all. The one page with no session
  left to read the account from — the same-origin hop that forwards the browser to the provider
  after sign-out, which runs once the session is already gone — takes it from its own sealed
  cookie instead, as "A sign-out record that belongs to nobody can no longer be written" below
  describes.
- **A sign-out record that belongs to nobody can no longer be written.** Deleting an account erases
  these records by account, so one stored without an owner would outlive the account it came from,
  keeping the provider token for up to a week after the erasure. Both the place that creates one at
  sign-in and the storage layer beneath it now refuse to write a record with no owner.
