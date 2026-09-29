none

Test-only: a `releasegate` test now reads the `publish` job of `docker-image.yml` and pins the
edge that puts the release gate in its way. `needs:` must contain `verify-release-tag`, the job's
`if:` must be exactly the form that lets a tag through only on the gate's `success` (and a
`skipped` gate only off the tag path), and the gate's own `if:` must stay
`github.ref_type == 'tag'`. Only whitespace and the `${{ }}` wrapper are normalised; a shape the
reader cannot follow — a duplicated key, a continuation line, a block or quoted scalar, an
unwrapped condition that YAML would read as a tag — fails the test rather than passing it. The
comparison with `ci.yml`'s `publish-image` reads its `needs:` through the same reader.
