### Internal

- **`brace-expansion` lifted to 5.0.9 in the lockfile**, closing CVE-2026-69152
  (HIGH) and turning the required `trivy-fs` check green again. The package is a
  transitive dev-only dependency of the build toolchain and is not part of any
  shipped artifact, so no running instance was exposed. The `overrides` entry
  stays the floor `^5.0.8` — 5.0.9 satisfies it, so only `package-lock.json`
  changed.
