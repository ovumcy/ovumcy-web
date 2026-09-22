none

Test-only (WEB-17 TS-L6): `e2e/calendar.spec.ts`'s two custom-onboarding
scenarios (the BBT-anovulatory demotion and the fertile-tier fill check) each
duplicated the same register + step-1/step-2 onboarding block; collapsed into
one shared helper, `registerAndOnboardOnDate`. The anchors, the ovulation
legend/grid assertions, the delete-confirmation banner check, and the
positive-anchor pattern this spec already used were checked against the
current calendar template and are current — no stale selector, fragment, or
conditional found there. The phase-cell test pinned to month day 1 (#825) was
re-run with the page clock pinned to day 1 and to day 2 of the current month
and passes both.
