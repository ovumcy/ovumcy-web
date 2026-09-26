none

CI-only: `e2e-cross-browser` gives firefox triple the per-test timeout on
`cross-browser-smoke.spec.ts` (WEB-79). Evidence (four reddened push runs) showed
the app answering every request and resource in under 300ms; the delay was the
browser/driver load-event signal on firefox's first post-launch navigation, not
an application hang. No operator-visible behavior changed.
