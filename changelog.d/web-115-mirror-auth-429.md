### Internal

- **The Docker Hub mirror waits out Docker Hub's rate limit instead of failing the publish on it.**
  Two pushes to `main` on 2026-09-29 went red in `publish-image` because a `cosign copy` in the
  mirror step was answered with `429 Too Many Requests` during a merge burst, after the mirror
  login had succeeded and, in one of them, after the same step's pushes to Docker Hub had been
  accepted. Every copy in that step now asks again on that one verdict, up to four attempts with a
  growing pause, and on nothing else: any other refusal still fails the step on its first attempt,
  and a limit still standing after the last attempt fails it too, so the mirror is never reported
  published with a copy missing. The retry is matched on the registry's own wording rather than on a
  bare `429`, which a digest in an unrelated error can contain. `scripts/releasegate` runs the
  step's real script against a shimmed `cosign` and `sleep` to hold it to both halves and to the
  growing pause between attempts, and holds the shipped budget to more than one attempt with a
  non-zero pause.
