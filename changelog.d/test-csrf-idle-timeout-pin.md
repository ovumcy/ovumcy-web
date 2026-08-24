none

Test-only: pins the CSRF middleware's token idle timeout to the one hour the server
already configures, so a silent revert to fiber v3's 30-minute default cannot pass
green. No behaviour changes.
