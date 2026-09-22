none

Test-only (WEB-17 TS-L2): the step-up refusal AST guard now scans `applyClearData`,
`applyDeleteAccount` and `refreshCurrentSession` directly. `settingsStepupRefusalSpecs` derives the
specs those three helpers can raise from a new `inlineStepupSessionHelperSpecs` map instead of a
hand-typed block, and `TestStepupSessionHelperRefusalSpecsMatchTheHelperSources` cross-checks the
map against the helper sources in both directions — a spec newly raised inside any of the three, or
a stale map entry none of them names any more, now fails that guard by name.
