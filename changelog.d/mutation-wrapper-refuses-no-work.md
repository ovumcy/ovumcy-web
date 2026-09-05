### Fixed

- `scripts/mutation.sh baseline <slug>` no longer reports success when `<slug>` matches no
  registered target: it now exits non-zero and lists the valid plain and shard-form slugs instead
  of printing "baseline JSON written" having invoked gremlins zero times. `verify-shards` likewise
  refuses a registered sharded package whose directory has zero non-test `.go` files instead of
  treating an empty partition as a covered one.
