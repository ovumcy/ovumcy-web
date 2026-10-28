### Fixed

The browser timezone sync no longer posts from pages shown before onboarding
completes, where the onboarding gate refused it with a 403 on every load; the
dashboard reached after onboarding performs the sync. Its once-per-tab mark is
now kept per account, so a second account signing in to the same tab after a
sign-out syncs its own timezone instead of keeping an empty one, which left its
reminders and calendar feed on the instance zone.
