none

Internal-only correction with no behaviour change: the reminder scheduler's
documentation named the wrong mechanism for a real safety property. On a DST
fall-back day the local run hour occurs twice, and the comments claimed the
once-per-local-day "ran today" marker was what kept the repeated hour from
firing a second notify pass. It is not: the marker is read only by the startup
catch-up, never by the timer loop. What actually prevents the second fire is the
schedule math rebuilding its candidate strictly after the instant that just
fired, which rolls the next fire to the following calendar day. The three places
that repeated the claim now name the rollover, and a new regression drives the
scheduler loop itself across a repeated wall-clock hour and pins exactly one
pass.
