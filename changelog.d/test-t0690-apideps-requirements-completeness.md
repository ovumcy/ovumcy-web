none

Test-only. A dependency of the HTTP layer is declared in three places — the
`apideps.Dependencies` field, the `requirements()` row whose absence makes
startup fail fast, and the bootstrap assignment that wires it — and only the
middle one is a check. Nothing bound the three together: the existing test
validates the zero value, and `Validate` reports the FIRST missing requirement
and returns, so it stayed green no matter how many rows `requirements()` had
dropped. A field added to the struct and forgotten in the inventory lost its
fail-fast silently, turning a clear startup error into a nil dereference on the
first request that reached the service.

The new sweep binds rows to fields behaviourally, in both directions: every
dependency field, nilled alone inside an otherwise fully wired `Dependencies`,
must make `Validate` report an error, and the errors so produced are all
distinct while `requirements()` lists exactly as many rows as there are
dependency fields — so rows and fields are in bijection, and a stale or
duplicated row is a count mismatch rather than dead weight. Exemptions are an
explicit list carrying the reason each field cannot be a requirement
(`AuditLogEnabled` is runtime config, where `false` is a legitimate value); a
field the sweep cannot wire fails it instead of being skipped, and an exemption
naming a field the struct no longer declares fails too. Production behaviour is
unchanged — the 24 fields and the 24 rows already agreed, by hand, and no tag or
naming convention had to be added to `requirements()` to make the binding
checkable.
