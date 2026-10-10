### Internal

- **`source-map-js` lifted to 1.2.2 in the lockfile**, closing CVE-2026-93749 (HIGH) and turning
  the required `trivy-fs` check green again. The package is a transitive dev-only dependency of
  the CSS build and of `jsdom`, not part of any shipped artifact.
