### Internal

- **`TESTING.md`'s sealed-cookie section now matches the codec that ships.** It said eleven AEAD
  purposes and its table omitted `ovumcy_calendar_feed`, which the codec security test has
  exercised since the `.ics` feed landed. The mutation section's claim that `internal/api` is the
  largest package went with it: `internal/services` is bigger and is sharded the same 5 ways. The
  header's test counts were corrected in the same pass; they are dropped entirely by the entry
  below, which supersedes that half.
