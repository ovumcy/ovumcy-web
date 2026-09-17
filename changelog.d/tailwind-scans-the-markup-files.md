none

Build-input scope only: the hermetic Tailwind `@source` set named the
`internal/templates` package directory, so `embed.go` and the package's
`*_test.go` files were CSS inputs too and any word of their prose could mint a
utility into the shipped bundle. The entry now names the markup files
(`internal/templates/**/*.html`), matching the `internal/httpx/markup.go` entry
beside it. The built bundle is byte-identical to the one already committed, so
nothing user-visible changes.
