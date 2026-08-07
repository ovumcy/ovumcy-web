### Changed

- **The backup and restore helper in `docs/self-hosted.md` pins the same Alpine release as the
  runtime image again.** Both were moved to `alpine:3.22.3` in one commit, to clear the OpenSSL
  findings that failed the image scan; the Dockerfile has since been carried to `alpine:3.24.1` by
  the automated base-image updates, which read `Dockerfile` and compose manifests but not a fenced
  command in a Markdown runbook, so the documented helper stayed three minors behind on an image
  pinned for a security patch. The commands themselves — BusyBox `sh`, `tar czf`/`tar xzf` — need
  nothing from either release, so the older pin bought an operator nothing but the unpatched
  packages it was originally raised to escape.
