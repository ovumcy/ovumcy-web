none

Test-only, and a narrowing of the browser-error fixture's per-test allowances rather than a change
to it. The day-save resilience specs break a save on purpose, so htmx logging that failure is the
behaviour under test and is tolerated. The refusal log names the URL and is scoped to the very path
each interception routes; the connection-drop log carries no URL and cannot be scoped that way, so
it is scoped by **arming** instead — raised the first time an interceptor is switched to
`network-failure`, and never in a test that stays on the refusal outcome and drops nothing. Before
this, the one spec that never aborts a request still tolerated a dropped connection from any
endpoint, which would have let a real one pass unreported: these specs assert about the day form and
nothing else. The refusal allowance stays unconditional on purpose, because an interceptor can
refuse from its first request.
