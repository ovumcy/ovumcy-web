none

Test-only barrier, no user-visible change: a sweep now fails when a file outside
`internal/services/dashboard_cycle.go` builds a prediction-suppression gate out
of two or more of its signals, instead of calling `PredictionsSuppressed` /
`FertilityProjectionSuppressed` or reading the decision the cycle context has
already resolved. The rule it enforces is not new — it has been written down for
three release waves and breached in each of them, caught by review every time.

It flags one boolean expression naming two distinct signals, which is a
hand-built gate; a single signal beside an ordinary operand — a count, a date, a
settings flag — is a surface asking its own question and stays legal, and
flagging those would red every honest call site. One site is carried as a
sanctioned residual with its reason: the dashboard's first-cycle bridge line
names no date, so no gate withholds it, and it cannot read the fertility gate
because the first-cycle floor is the state it is shown in. A residual forgives
exactly one recombination in the function it names, and only of the signals the
entry was written about: a second gate there, or the same site rewritten to
combine a different pair, is reported beside it rather than inheriting an
exemption whose stated reason no longer describes it. Sites are keyed by path
and function, with a method's receiver type included, so two same-named methods
in one file stay two sites.

Both swept layers are walked to their leaves rather than one directory deep: a
sweep that stops at the top level answers about today's file layout instead of
about the class, and would go quietly green the day these files are split into
packages.

Two limits are stated in the barrier and repeated in its failure text. A
recombination assembled through intermediate variables reads as one signal per
expression and passes. So does one built in a template: the owner templates are
handed `PredictionDisabled` and no second signal today, so a template cannot
currently spell a second disjunct, and the day one is handed over is the day the
sweep stops covering the surface that reads it.
