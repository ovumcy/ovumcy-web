### Internal

- **Docs: the declared exceptions to "no usable secret in transport" read the same in SECURITY.md
  and docs/SECURITY_INVARIANTS.md.** Two exceptions (calendar-feed token, TOTP enrollment seed);
  a query-mode OIDC code is inert, not an exception. Each names its pinning test; the seed is
  described as minted fresh per visit, not "shown once".
