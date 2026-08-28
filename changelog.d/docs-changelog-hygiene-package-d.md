### Internal

- **`CHANGELOG.md` and eight `changelog.d/` fragments stop misdescribing what shipped.** Accuracy
  pass from the 2026-08-28 documentation audit (package D). In `[Unreleased]`: the release-image
  publish entry no longer claims tagged releases "publish from their own tag push as before" — the
  release-image-gate fragment landing in the same version makes that untrue, since a tag now gates
  on the same checks `latest` waits on; and the OpenAPI-exemptions entry no longer counts the
  calendar feed's per-IP `429` among the bare-body exemptions — a later entry in the same
  `[Unreleased]` section moves that `429` onto the shared envelope, leaving only the feed's `404`
  path exempt. In `changelog.d/`: a stray `</content>` tag dropped from the API-duplication sweep's
  fragment; a fragment whose entry had become moot rewritten to the bare `none` marker; a
  plain-paragraph entry wrapped as a bullet to match every sibling entry's form; two cross-reference
  fixed to name their target instead of a filename-order-dependent "next entry" / "the entry below"
  (one of which pointed the wrong direction once fragments are sorted by filename); the release-image
  gate's Dependabot claim corrected — the pre-release-tag gap is closed by a CI check, not a
  Dependabot `ignore` clause, which `dependabot.yml`'s own comment explains cannot express "no `rc`
  tags" without risking every ecosystem's feed; three source-line counts re-measured against the
  current tree; and a dark-theme fix's cycle-ring contrast numbers marked superseded by the wave-4
  cycle ribbon that replaced the ring, with its corner-glow claim corrected now that a later pass
  gave the dark theme its own tinted glow instead of leaving the corner bare. No response, template,
  or behavior changes; the link-block item this audit package also named (F0) had already shipped in
  #629.
