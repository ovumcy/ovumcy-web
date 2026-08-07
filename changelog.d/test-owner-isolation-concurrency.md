### Internal

- **Owner isolation is now proved under concurrency, not only statically.** On an instance hosting
  two independent owners, twelve workers across the two sessions write and read the same dates at
  once; every response is checked for the other owner's rows, identity and cookies — in the body
  and in `Set-Cookie` — the persisted rows are re-read afterwards, and a positive anchor counts the
  own-data reads so a run that returned nothing cannot pass as "no leakage". Green on Windows and
  under the race detector on Linux.
