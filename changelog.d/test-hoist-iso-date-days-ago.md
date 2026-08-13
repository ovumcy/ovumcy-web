none

Tests only: the last hand-rolled `isoDateDaysAgo` in the e2e auth helper is gone.
The ISO-date arithmetic the helpers share now lives in one dependency-free e2e
module, which is what lets the auth helper reach it without importing the stats
helper back and closing an import cycle between the two. No product code.
