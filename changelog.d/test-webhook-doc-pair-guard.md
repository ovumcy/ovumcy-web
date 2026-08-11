### Internal

- **Four documents restate the webhook egress contract, and nothing failed when a change moved
  three.** A classifier or delivery-envelope change touches `SECURITY.md`, its public mirror
  `docs/SECURITY_INVARIANTS.md`, `docs/notifications.md` and `docs/self-hosted.md`; one change
  moved the matrix and left the mirror stale for a day, and only a follow-up caught it. The new
  `scripts/webhookdocs` guard checks what each pair genuinely owes rather than demanding the four
  read alike: every row of the matrix's webhook section is either mirrored in the public document
  or explicitly declared matrix-only with a reason, every test a row cites is defined in a file
  that row links, and the two operator documents state the private-address gate together with the
  default read out of the code that parses it. The row set is swept out of the document, so a row
  added later cannot join without a verdict. The obvious shape — every address form named in every
  document — was measured first and refused: the four name 1, 1, 4 and 1 of the six IPv6
  transition prefixes, because they are written for different readers.
