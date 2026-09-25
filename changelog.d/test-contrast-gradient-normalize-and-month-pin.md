none

Test-only: `e2e/a11y-wcag-audit.spec.ts` gets a new case proving the WCAG contrast helper
(`measureTextContrast`) normalizes a `background-image` gradient's stops instead of silently
missing them — white text on a same-colour white gradient must measure the true 1:1 ratio, the
counterexample a helper that dropped the gradient layer would misreport as "no background" (throw)
or, if the extraction regressed differently, as compliant.

The existing "calendar phase cells" case is also de-flaked: it anchored the seeded cycle start at
`today - 6` days, so on a run date near a month's start or end the period cell and the predicted
fertile window (days 9-14 of the cycle on the 28-day/14-day-luteal defaults) could straddle two
calendar months and the bare `/calendar` default view would show neither — the month-boundary flake
class first seen in #620: the grid renders only the viewed month padded to whole weeks, so a
today-derived date near a month's edge can be unrendered. The seed is now anchored at day 1 of the
current month and the view navigates to that month explicitly, so the whole window sits inside one
month's grid on every run date. No production behavior changes.
