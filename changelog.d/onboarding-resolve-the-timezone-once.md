none

Internal only: the onboarding step-2 handler resolved the request timezone
twice — once discarding the result, once inside the completion call — and now
resolves it once into a named variable that both uses. Behaviour is unchanged
on every path: the resolver's only side effect is writing the `ovumcy_tz`
response cookie, it reads the request and never its own response, so the two
calls always took the same branch and returned the same zone. What the discard
bought is that the cookie is written before step 2 can refuse the submission,
which the named variable keeps by staying where the discard was; a regression
now pins it, so the resolution cannot drift down to the completion call site.
On the urlencoded path this also stops the request body being materialized and
re-parsed a second time per submission.
