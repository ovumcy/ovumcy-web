### Internal

- **The security documents now state, and map to their tests, the rule that predictions are
  withheld rather than softened.** The Test Enforcement Matrix is the source-of-truth mapping from
  a claim to the Go test guarding it, and its medical-safety section listed only the disclaimer's
  wording — while the other half of that invariant, withholding a window the recorded data cannot
  support, had enforcing tests and no row. `docs/SECURITY_INVARIANTS.md` had the matching gap: its
  medical-safety section stated the disclaimer alone, though production code and the sweep's own
  failure text send readers there for the suppression floor, so the pointer resolved to a document
  that did not carry the claim.

  Both are fixed. The matrix row credits both tests that hold it — the sweep that fails when an
  expression outside the file declaring the predicates combines two suppression signals by hand,
  and the check keeping that sweep's list of signals answerable to the predicates themselves — since
  a claim held by two tests that credits one invites the deletion of the other. The row also states
  what the sweep cannot see: it reads one Go expression at a time, so a gate assembled across
  intermediate variables, or in a template, is outside its reach.

  Both documents state the rule at one strength, and it is the strength that holds today: every
  surface asks one predicate per half rather than rebuilding the rule, so a new signal is added
  once. Neither claims that the surfaces therefore agree — they feed that predicate different
  inputs, and reconciling those is a separate change.

  No behaviour, test or invariant changes.
