none

Test-only guard: the export service's owner scoping is now observable. Both export
stubs used to declare an unnamed `uint` and record nothing, and every integration
fixture held a single owner, so an export that fetched a constant account's days or
symptom catalog passed the whole suite. The stubs now record the owner operand of
every read and the unit test asserts it across all four entry points — LoadDataForRange,
BuildSummary, BuildJSONEntries, BuildCSVRows — with a positive anchor per collaborator,
so a run that reached one of them fewer times than expected fails instead of reporting
"no mismatch found". Alongside it, an integration case seeds two independent owners on
one database, exports as the later-created owner and asserts that neither the other
owner's day rows nor the other owner's symptom names appear, while each owner still
exports their own data. No behaviour changes; the read path already carried the owner
correctly.
