### Fixed

A form submitted without JavaScript after the session ended — a calendar or
dashboard day save or delete, the cycle-start mark, an onboarding step, or one of
the settings forms — no longer lands on a bare refusal page with no layout. The
browser is sent to the sign-in page, which shows the "not signed in" notice in the
interface language. Any other refusal of those forms — an expired form token, a
value the form cannot save, a server error — keeps its status, its message and
its link back to the form, and is now shown as a full page in the app's layout
and in the interface language. The same holds for a refused sign-in, registration,
password-recovery or two-factor form and a refused language switch: the same
status, message and link back, shown as a full page instead of a bare fragment.
htmx requests and JSON API clients keep their status and the same body as before.
