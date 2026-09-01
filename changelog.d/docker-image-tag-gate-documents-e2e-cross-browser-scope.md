none

Re-verified REL-5 from the release audit against current HEAD (post the release-gate rework in
PR-2/PR-3): the `REQUIRED_CHECKS` array it cited no longer exists — that mechanism is now
`docker-image.yml`'s `QUEUE_CHECKS`/`HEAVY_CHECKS` split, which deliberately mirrors exactly the
five checks `publish-image` requires for `:latest` (ci.yml:1706-1711). `e2e-cross-browser`'s
absence from both is consistent with its own documented non-required status (ci.yml:1512-1513),
not a second instance of the same gap. Adding it to the tag gate alone would make a release tag
stricter than `:latest`, which is a product-policy call outside this audit's scope, not a P2 fix.
Added a comment at `docker-image.yml:156-157` recording this so a future reader (or a re-audit)
doesn't re-flag it as an oversight. No branch-protection change needed.
