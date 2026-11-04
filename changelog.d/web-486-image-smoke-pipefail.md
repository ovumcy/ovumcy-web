none

CI reliability change with no user-visible effect: the image smoke's
cache-header check no longer pipes into `grep -q`, so a header that matched
can no longer fail the step on a broken pipe under pipefail.
