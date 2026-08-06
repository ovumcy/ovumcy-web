### Internal

- **`TESTING.md`'s counts and package claims now match the suite that ships.** The sealed-cookie
  section said eleven AEAD purposes and its table omitted `ovumcy_calendar_feed`, which the codec
  security test has exercised since the `.ics` feed landed; the header figures said 27 Playwright
  specs and 2,150+ Go test functions against 29 and 2,333; and the mutation section called
  `internal/api` the largest package when `internal/services` is around 40% bigger and is sharded
  the same 5 ways.
