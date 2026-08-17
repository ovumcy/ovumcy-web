none

No user-visible change: the settings unsaved-leave guard keeps prompting and
keeps sending nothing, and the reason it does not flush the way the dashboard
journal does is now written down beside it. The settings cards are an explicit
Save/Discard surface whose prompt asks whether to discard, so a flush would
persist exactly what the owner chose to throw away — and would put a
configuration the cycle guidance never validated in front of the prediction
surfaces. A new unit test (`settings-unsaved-leave.test.mjs`) pins that no
request leaves through `fetch`, `sendBeacon`, `XMLHttpRequest` or a native form
submit while the page unwinds, and that no destructive or credential-bearing
form (password change, data wipe, account deletion, webhook URL, calendar feed)
is part of the dirty-tracked draft shell.
