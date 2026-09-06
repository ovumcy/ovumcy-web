### Internal

- **The pre-032 calendar-feed backfill is now pinned not to run on a refused token.**
  `ResolveFeed` verifies a row minted before migration 032 through bcrypt and, on success,
  writes that row's MAC in from the presented verifier — the only moment the verifier plaintext
  exists. The refusal itself was already covered one layer down, where a tampered token is
  checked against a hash-only row; what nothing observed was the refusal at `ResolveFeed`, which
  is where the backfill sits: every backfill case there presented the correct token, and the
  bad-token sweep runs against an armed post-032 row whose non-empty MAC skips the branch, so a
  write reached without a passing verification would have installed a wrong verifier as the
  row's authenticator with the suite green.
  `TestResolveFeedRefusesAWrongVerifierOnAPre032RowAndWritesNoMAC` presents the real selector
  with a wrong verifier and pins the refusal — no feed, no error — the absent owner-scoped log
  read and the untouched row, with a positive anchor in the same case that serves the correct
  token and compares the stored MAC with the one generation derives. The `SECURITY.md` row for
  the pre-032 fallback cites it. No production change.
