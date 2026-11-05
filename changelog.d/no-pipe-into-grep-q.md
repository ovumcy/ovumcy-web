none

CI reliability change with no user-visible effect: a guard now refuses any
pipefail workflow step that pipes into a quiet grep, and the image smoke's
probe and cache-header checks and the publish job's `latest` checks match
captured output instead, so a check that matched can no longer fail on a
broken pipe.
