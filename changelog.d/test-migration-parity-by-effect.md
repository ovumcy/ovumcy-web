none

Tests only: the dialect-parity guard now looks at what a migration does, not
only at what it is called. A migration file that runs no statement with an
effect has to be declared as a deliberate no-op with its reason, and the
`daily_logs` tracking contract — columns plus nullability — is asserted from one
shared list on both engines instead of two per-dialect lists that had drifted
apart. No product code, no migration changed.
