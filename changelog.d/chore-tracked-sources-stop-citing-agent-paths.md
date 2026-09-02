none

Comment-only fix: twenty-one comments in Go, workflow and Playwright sources
cited an agent-only rule file — some by path, some by bare filename, which is
why a sweep for the path spelling alone had found only half of them. A tracked
source must never name one. None of the twenty-one was a security-invariant
claim, so none could be redirected to `docs/SECURITY_INVARIANTS.md` without
asserting something that document does not say; each comment now restates the
constraint it relies on, leaving the tracked source self-contained. One site is
deliberately kept: the locale reachability test writes a fixture under an
agent-only directory as DATA the walker under test must skip, not as a citation.
No behaviour change.
