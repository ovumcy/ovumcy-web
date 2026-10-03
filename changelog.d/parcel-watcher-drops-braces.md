### Dependencies

- Overrode `@parcel/watcher` (pulled in at 2.5.1 by `@tailwindcss/cli`) to `^2.6.0`, which
  matches globs with `picomatch` instead of `micromatch`. This drops `braces@3.0.3` and its
  unfixed CVE-2026-93687 (stack-exhaustion DoS, no patched release) from the frontend build
  toolchain. Development dependency only; the shipped binary and runtime image are unaffected.
