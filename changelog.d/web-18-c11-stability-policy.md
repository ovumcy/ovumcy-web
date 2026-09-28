### Changed

- **The API stability policy now matches how v2.0.0 ships.** `CONTRIBUTING.md` and the
  `Stability` block of `docs/openapi.yaml` used to promise that a breaking change would arrive
  under a parallel `/api/v2/*` prefix. There is no `/api/v2/*` and none is planned: a breaking
  change ships in a new major release on the same `/api/v1/*` prefix, with a **Breaking** entry
  in this changelog naming what a client has to change. A correction that makes the spec describe
  what the server already does is listed under **Fixed**, not as a break. Every `/api/v1/*` break
  v2.0.0 makes in place now carries a **Breaking** marker in this changelog, and `info.version` of
  the spec is now `2.0.0`.
