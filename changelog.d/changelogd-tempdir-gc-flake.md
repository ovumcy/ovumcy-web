none

Internal: the changelogd test repositories turn off git's auto-gc and
maintenance, so a detached gc can no longer race t.TempDir cleanup over
.git/objects/pack and fail a passing test.
