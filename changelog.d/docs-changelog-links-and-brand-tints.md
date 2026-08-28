### Internal

- **The changelog's link-reference block knows about 1.9.1 and 1.9.2.** `[Unreleased]` compared
  `v1.9.0...HEAD`, so its diff carried two released versions' changes as if unreleased, and the
  `[1.9.1]`/`[1.9.2]` headings rendered as literal bracketed text with no target. Both patch
  releases are tagged; the block now defines them and `[Unreleased]` starts at `v1.9.2`.
  Assembly never maintained this block — it is a per-release manual step that was missed twice.
- **Release 1.3.0 is dated 2026-06-15**, the date of the tag and of the very commit that wrote
  the heading (`63ad241f`); it was written two days early at authorship. Every other release
  heading matches its tag.
- **`[Unreleased]` carries one `### Added` section, not two.** The OIDC self-erasure entry sat
  in a second `### Added` after `### Changed`, which the declared Keep a Changelog format does
  not do. Assembly already merged repeated headers, so this only ever affected the rendered
  file — the entry text is moved, not edited.
- **The brand README documents the dark-background tints.** `ovumcy-icon-dark.svg` and
  `ovumcy-logo-horizontal-dark.svg` use `#CFBBFF` and `#FFA2AF`, which the palette never
  mentioned — a designer recreating a dark asset from this file reached for the light-background
  values.
