### Internal

- **The per-operation status guard now judges redirects instead of excluding them.**
  `TestOpenAPIOperationsDeclareEveryStatusTheirOwnHandlerChainCanEmit` listed `303` as
  cross-cutting, on the argument that any handler may reach the shared content-negotiation helper
  regardless of its own logic. That is true of the helper and false of a handler that falls through
  to `c.Redirect()` with nothing answering the JSON caller first — which is how both 2FA mutations
  redirected an `Accept: application/json` client to a page, undeclared, on a green `main`. The
  exclusion is gone and the surface question is answered where it is asked: the walk now reads an
  `if acceptsJSON(c) { … return … }` as ending the JSON caller's request, so later statements in
  that block are the HTML surface, and reads `switch responseFormat(c)` the same way, walking only
  its JSON arm. Dropping the exclusion without those two reddened 20 operations, 18 of them falsely.
  - Two things stop the quieter guard from being a blind one. A new test,
    `TestStatusReachTellsARedirectJSONCallersGetFromOneTheyDoNot`, runs the analyser over source
    written for it — a bare fall-through, an `acceptsJSON` early return, the shared switch, and an
    `acceptsJSON` arm that does *not* return — and each case also asserts a status the walk must
    still find, so a run that reached nothing cannot pass as a run that found nothing. The sweep itself now fails when it
    judged fewer than 40 `/api/v1` operations, because a route table that failed to load and a
    clean tree print the same nothing.
