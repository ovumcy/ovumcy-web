### Internal

- **`THIRD_PARTY_LICENSES.md` now delivers the completeness its scope sentence promises.** The
  file claimed to list the third-party software redistributed in the built application while
  inventorying only htmx and dismissing Tailwind as "not redistributed" — the shipped
  `tailwind.css` is git-tracked, embedded via `go:embed` and served with Tailwind's MIT banner
  preserved on line 1, and ~45 Go packages under MIT/BSD-3-Clause/Apache-2.0 notice obligations
  were statically linked into the released binary with no attribution anywhere. The file now
  carries a generated Go-module table (one row per license-bearing package of the binary's build
  graph: package, license, link to the pinned version's license text), produced by the new
  `scripts/licensesdoc` from a `go-licenses` report and verified by a new `test-go-analysis` CI
  step that regenerates the report and refuses a stale committed block — so a dependency bump
  cannot silently un-true the inventory again. The Tailwind entry now states the compiled file
  IS shipped and that its preserved `/*! tailwindcss … MIT License */` banner is the notice, and
  a runtime-image section attributes the two data artifacts the scratch image copies in: the
  Mozilla CA certificate bundle (MPL-2.0) and the IANA tz database (public domain). The one
  package `go-licenses` cannot classify (`modernc.org/mathutil`, standard BSD-3-Clause text) is
  hand-classified through a refuse-by-default override table. No behaviour changes.
