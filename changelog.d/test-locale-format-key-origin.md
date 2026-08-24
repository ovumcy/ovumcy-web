none

Test-only barrier, widening the catalogue printf guard that shipped alongside
it. That guard compared a locale string's verb sequence against the argument
list its `fmt.Sprintf` passes, but it could only check the catalogue key at the
site's own `lookupMessage`. Six of the eleven keys are not written there: the
basal-temperature range strings are chosen by the dashboard helper two frames
above the call, the cycle-label key is looked up in the transport layer and
formatted in the stats service, the day-save key arrives through a local
variable, and the webhook reminder keys through package constants. Those six
were declared in the contract table and compared against nothing.

A contract now names where its key is written — the string literal at a given
argument of a given callee, optionally narrowed to one function — and the reader
also follows a key one hop through a local variable or a package constant. So a
call repointed at another catalogue key fails here, which is the drift that
renders `%!d(string=35.0)` into the basal-temperature range hint while every
other locale barrier stays green.

Two smaller corrections to the same barrier: it refuses a file that declares one
function name twice, rather than merging both functions' format sites into one
bucket and reporting findings that name neither; and contracts are matched to
call sites by the key set each site names rather than by their order in the
table, so reordering the table can no longer repoint an entry at another site's
argument list.

No user-visible change: all eleven keys still agree with all nine sites in all
six languages.
