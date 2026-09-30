### Dependencies

- Raised the `brace-expansion` override from `^5.0.8` to `^5.0.11` (lockfile
  now at 5.0.12), clearing CVE-2026-102276 and CVE-2026-102278 in the frontend
  build toolchain. Development dependency only; the shipped binary and runtime
  image are unaffected.
