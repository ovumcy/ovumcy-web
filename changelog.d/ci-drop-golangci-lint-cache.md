### Internal

- **CI no longer caches `golangci-lint`'s analysis cache.** The cache key could
  only name `go.sum` and `.golangci.yml`, never the source tree, so a branch
  whose dependencies were unchanged but whose commits had moved was linted
  against facts computed from a different tree — which produced six
  deterministic phantom `staticcheck SA5011` findings on a dependency bump that
  reproduced on no other machine. Restoring the cache was measured at 1–5 s on a
  job of about six minutes, so removing it costs effectively nothing.
