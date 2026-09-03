### Fixed

- **Ovulation-impossibility claim no longer survives fertility suppression.** `GET /api/v1/stats/overview` (and every other surface fed by the shared published-stats adapter) used to publish `ovulation_impossible: true` even while `suppression.fertility` was also `true` — a claim derived from the same fertility projection the suppression withholds. It is now cleared alongside the ovulation date and fertile window whenever fertility is suppressed.
