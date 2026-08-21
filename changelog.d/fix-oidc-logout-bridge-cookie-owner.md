### Security

- **The page that forwards you to your identity provider after sign-out now names the account it
  acts for.** That page runs when your session is already gone, so all it had to go on was the
  session identifier carried in its own sealed cookie — and it looked the stored sign-out record up
  by that identifier alone. On an instance hosting several independent owners, a cookie carrying one
  owner's browser and another owner's session identifier would have been handed the second owner's
  provider token and consumed their record in passing. The cookie now carries the account as well,
  and the record is found only by the two together: a mismatched pair gets an ordinary local
  sign-out and leaves the other owner's record untouched. A cookie naming no account at all is
  refused and cleared rather than treated as naming any account.
- **A sign-out started in the minute before this update finishes locally instead of at the
  provider.** The sealed cookie behind that page is valid for one minute, and one minted by the
  previous version carries no account. Rather than guess, the server refuses it, clears it, and
  signs you out here; the provider session ends at its own expiry or on the next sign-out. Nothing
  is left readable in the browser, and only cookies minted inside that one-minute window are
  affected.
