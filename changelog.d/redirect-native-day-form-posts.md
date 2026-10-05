### Fixed

A form submitted without JavaScript after the session ended — a calendar or
dashboard day save or delete, the cycle-start mark, an onboarding step, or one of
the settings forms — no longer lands on a bare refusal page with no layout. The
browser is sent to the sign-in page, which shows the "not signed in" notice in the
interface language. htmx requests and JSON API clients keep their `401` and the
same body as before; every other refusal of those forms is unchanged.
