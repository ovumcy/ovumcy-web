none

Test-only. Two recognisers in the symptom-id dedup sweep read a spelling where
they meant to read a meaning, so each cleared the case it exists to catch. No
production code changes.

The check behind an exemption asks whether the cleared loop counts anything,
because an exemption states that a repeated id changes no output. It knew
`m[k]++` and `m[k] += 1` and not `m[k] = m[k] + 1`, so the export flag builder's
exempted loop could start tallying repeats and the barrier stayed green. It now
reads the step rather than the operator, in either operand order.

Alias resolution counted assignments by identifier name over a whole function,
with no scoping, so two `:=` bindings of one name in sibling branches read as
one local assigned twice and the resolution was dropped for both — which
silently un-recognised a counting loop whose slice happened to be held in a
variable named like another. It is now keyed on the parser's own object, so the
two are two locals; an identifier the parser did not resolve is skipped, leaving
the range expression as written.

Both cases are pinned as fixtures the test owns, beside the ones already there.
