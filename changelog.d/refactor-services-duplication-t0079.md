### Internal

- **A day's symptoms are counted once at every read site.** Three of the six
  surfaces that count symptoms per day walked the stored id slice through a
  dedup helper and three walked it raw, so a slice repeating an id would have
  counted that day twice — a phase percentage above 100, a skewed frequency list
  and a skewed picker ranking. Validation already deduplicates before persisting,
  so nothing rendered wrong; the read side no longer depends on that. There is
  now one dedup helper, a second that filters unknown ids on top of it, and a
  sweep that fails on the next bare loop the moment it is written. The sweep
  found a seventh site the audit had not, and it is exempted by name with its
  reason: the export flag builder writes booleans and a name set, where a repeat
  changes nothing. No rendered number changes.
- **The fuzz harness is held to one story across its three copies.** The native
  `go test -fuzz` targets, the ClusterFuzzLite harness behind the `gofuzz` build
  tag, and the build script's target list had to agree by hand, and the tagged
  file is the one file the default build, vet, the linters and coverage never
  see. A barrier now reads all three and fails when their target sets, seed
  corpora or oracle bodies stop agreeing.
- **DayHasData and the auto-fill candidate check share one field list.** The two
  predicates test the same eight day fields with opposite returns, and nothing
  held them together: a ninth field wired into one alone would have let the
  auto-fill cleanup delete a day carrying that manual signal. A barrier reads
  both functions and fails on the asymmetry, with Flow, CycleStart and
  IsUncertain carried as the documented exceptions they always were.
- **The stats page's sample-size thresholds say what each governs.** The pattern
  minimum and the trend-reliability threshold are both three and answer different
  questions about different quantities, so they stay two names with two doc
  comments, and each surface is now pinned at its own boundary. The
  historical-phase preference likewise has its scope written down: the drawn
  markers on the calendar and the cycle stack, not the prose that groups moods
  and symptoms by inferred phase.
- **Repeated mutation kills folded into the tables that own them.** Five
  round-*N* files restated assertions their coverage tables already made, with
  line-number comments pointing at code that has since moved. Every case they
  contributed uniquely moved into the surviving table first. One of those tables
  turned out to assert Russian plurals with a word that is identical in the "one"
  and "many" forms, so no teen case in it could fail; it now uses distinct
  sentinels. The four symptom-ranking tests named for four boundaries exercised
  one identical state, and are one table whose rows sit where their names say —
  three of the four are new coverage.
