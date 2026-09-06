none

Documentation-only follow-up to the operator-CLI fence refusal. `docs/gdpr.md`
and `docs/SECURITY_INVARIANTS.md` described a never-fenced instance's boot
pass disarming every armed calendar feed, without saying that `ovumcy users
delete` and a forced `ovumcy reset-password` now refuse on that same instance
instead of completing. Both gained a pointer to `docs/self-hosted.md →
Calendar Feed Restore Fence` for the refusal and its remedy, and the
`users delete --id` runbook there now says the command shares that gate. No
behavior changed.
