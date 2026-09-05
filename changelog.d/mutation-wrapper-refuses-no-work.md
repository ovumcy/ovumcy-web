### Fixed

- **`scripts/mutation.sh` no longer reports success for a baseline run that mutated nothing.**
  `baseline <slug>` exits non-zero and lists the valid plain and shard-form slugs when the slug
  matches no registered target, instead of printing "baseline JSON written" having invoked gremlins
  zero times. A shard number written with a leading zero (`internal_api_08`) is refused rather than
  silently accepted: the range test failed on the octal literal inside an `if`, where `set -e` does
  not apply, so the shard was admitted and then run as shard 8 modulo the shard count — a different
  slice under the requested name. The accepted set now matches the one the error message
  advertises. A registered sharded package with no non-test Go source is refused at the baseline
  site as well as in `verify-shards`, because an empty exclusion list turns a shard run into a full
  unsharded run of the package under that shard's name; and `verify-shards` now reports a
  registry entry whose directory is missing apart from one whose directory is empty.
