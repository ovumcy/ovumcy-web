none

Test-only: internal/api's shared test app now mounts the same calendar-feed
CSRF/rate-limit exemption production does, so the settings-regressions'
"armed feed serves" preconditions prove the full contract (200, no
Set-Cookie, VCALENDAR body) instead of only a status code. No production
code changed.
