none

Test-only. The symptom-id dedup sweep that shipped alongside the dedup fix
passed in three situations it was written to fail in, and this closes all three.
No production code changes; the dedup itself is unaffected and still closed at
every read site.

The exemption that clears the export flag builder's loop was keyed on the
function and skipped that function's whole body, so a counting loop added beside
the cleared one was never examined — and the export path is the one site with no
behavioural guard behind the sweep. An exemption now names the loop it clears,
must match exactly one, and fails if the loop it cleared starts incrementing
anything, which was previously only a comment.

The sweep matched a range expression by its source text, so an ordinary
`ids := logEntry.SymptomIDs` walked past it. A local assigned exactly once is
now resolved back to the expression it was assigned from; one assigned twice, or
bound by the `range` clause itself, is left alone.

Its refusal to report success on an empty sweep counted compliant and violating
loops together, so the six compliant sites kept the count above zero and a
broken matcher could have detected nothing while passing. The sweep is now
anchored on fixture sources the test owns: two that must be flagged, two that
must not, and both sides of the exemption rule.
