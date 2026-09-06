### Internal

- **The pre-032 calendar-feed path now has a regression for its refusal, not only for its
  acceptance.** `ResolveFeed` verifies a row minted before migration 032 through bcrypt and,
  on success, writes that row's MAC in from the presented verifier — the only moment the
  verifier plaintext exists. Nothing observed the other outcome: every backfill case
  presented the correct token, and the bad-token sweep runs against an armed post-032 row
  whose non-empty MAC skips the branch entirely, so a backfill reached without a passing
  verification would have installed a wrong verifier as the row's authenticator with the
  suite green. `TestResolveFeedRefusesAWrongVerifierOnAPre032RowAndWritesNoMAC` pins both
  halves — the token is refused like any other bad token, and the refusal leaves the row
  unmigrated — with a positive anchor in the same case so neither negative can pass on a
  fixture that never resolves. The `SECURITY.md` row for the pre-032 fallback cites it. No
  production change.
