none

CI/tooling only, with no user-visible effect: REL-5 implemented as originally written (owner
decision, 2026-09-03), reversing the 2026-09-01 audit note that had left it
out on scope grounds — that note argued adding the check to the tag gate alone would make a release
tag stricter than `:latest`; adding it to both gates in one change keeps them level instead.
`e2e-cross-browser` now joins `docker-image.yml`'s `HEAVY_CHECKS` (it runs only on
push/release/workflow_dispatch, the same event set) and `ci.yml`'s `publish-image` job `needs:` and
`if:` result check. A release tag and `:latest` now both hold for cross-browser coverage, not only
Chromium; both gates run slower as a result. Branch-protection required-checks in GitHub settings
is a separate, owner-only step, tracked outside this repo's tree.
