### Internal

- **The OwnerOnly invariant now states its exclusions where the rule is stated.** The published
  rule — every state-mutating `/api/v1/*` endpoint chains `handler.OwnerOnly` — was written
  without exception, while the five endpoints it does not cover (register, login, the 2FA
  challenge, and the two password-reset routes, all of which run before any session exists)
  appeared only as keys in a map literal inside a test. `docs/SECURITY_INVARIANTS.md` and
  `SECURITY.md` now name that set, the test builds its allowlist from the one place the set is
  declared, and a new guard fails when the document and the declaration disagree in either
  direction — so a sixth exclusion cannot be added in a diff that reads like a test-fixture edit.
  The exclusion set is unchanged and no route changed: the five were always registered without
  `AuthRequired`.
- **The invariant now cites the test that can actually fail on it.** It named the role matrix in
  `internal/api`, which observes `AuthRequired` and stays green when a route drops `OwnerOnly`;
  the route-table guard in `cmd/ovumcy` is what enforces the chaining, and is now what the
  document points at.
- **The architecture map names the OIDC query-mode callback and the background scheduler.** The
  trust-boundary diagram showed only the `form_post` callback, though a `GET` callback on the
  same path is registered and supported under `OIDC_RESPONSE_MODE=query`; and the layering
  section listed no entry point other than HTTP, though `internal/reminders` drives the daily
  webhook notify pass with no request behind it and `internal/cli` reaches services and
  persistence directly under operator shell access. Both are now on the map, with `internal/apideps`
  and `internal/testdb` recorded as deliberately omitted wiring.
