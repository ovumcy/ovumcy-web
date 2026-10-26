none

Test-only: the e2e dashboard clock pin now freezes local noon of today instead of the moment of the call, so a run starting just after midnight no longer keys the page onto the previous day. The calendar grid settle helper now waits for the refreshed grid to replace the one on screen before it waits for htmx to be idle, so the idle check cannot pass in the gap before the cascade starts. No product code changes.
