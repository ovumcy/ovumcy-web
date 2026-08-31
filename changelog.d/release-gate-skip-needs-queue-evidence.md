### Fixed

- **A release tag can no longer be published off a green that ran nothing.** Before a `vX.Y.Z` tag
  builds and signs an image, a gate re-derives the verdict CI left on the tagged commit: it must be
  contained in `main`, and the five checks that hold the rolling `:latest` back — `test`, `race`,
  `e2e`, `e2e-postgres-smoke`, `image-smoke` — must have passed on it. A tag push starts no CI run
  of its own and branch protection guards merges rather than tags, so this gate is the whole of the
  release path's judgement.

  It was reading three of those five in the one place where they carry no verdict. A commit on
  `main` is judged twice — once by the merge queue on the identical revision, once by the push that
  follows the merge — and the push run deliberately clears the unit, race and browser lanes,
  because the queue has just run them on that exact tree. Their gate jobs still report, and still
  report green, in a handful of seconds with every lane beneath them skipped. The gate consulted
  only the push run, so `test`, `race` and `e2e` cleared a release tag on a result that says
  nothing about the commit, and would say the same for any commit. It also accepted `skipped` from
  all five alike, which let a lane that never ran stand in for one that passed. The two readings
  contradicted each other: the queue's verdict was refused as untrustworthy, and then a push-run
  green whose only justification was "the queue already ran it" was accepted in its place.

  Each check is now read where it actually executes. `test`, `race` and `e2e` are taken from the
  merge-queue run on the same revision, and a commit that reached `main` without one is refused.
  `e2e-postgres-smoke` and `image-smoke` are the lanes the queue does not run, so they are taken
  from the push, release or manually dispatched run. `success` is the only conclusion that clears a
  tag in either half — `skipped` and a still-running check are both refusals now — and a failure
  anywhere still fails the workflow with no bypass, so a tag that cannot be verified does not
  publish.

  A refused tag says which half refused it, because the two have different remedies. A missing or
  unfinished push run is waited out, or produced by running CI on the commit by hand. A missing
  merge-queue run is not: that run exists only for a commit the queue built, so nothing an operator
  starts will create one, and the answer is to tag a commit that reached `main` through the queue.
  Either way the tag is deleted and re-pushed once the evidence is there.
