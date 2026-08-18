none

CI reliability change with no user-visible effect: the Playwright
system-package install steps are now time-bounded, so a wedged apt mirror
fails the step in minutes instead of hanging until the job's 45-minute
timeout.
