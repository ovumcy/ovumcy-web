### Dependencies

- Added a `minimatch` override at `^11.0.1` (lockfile now at 11.0.3), clearing
  GHSA-g3hv-75wg-m249 (CVE-2026-118764) in the frontend lint toolchain. The
  patched release exists only on the 11 line. Development dependency only; the
  shipped binary and runtime image are unaffected.
