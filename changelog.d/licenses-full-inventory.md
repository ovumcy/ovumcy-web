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
  a runtime-image section attributes what the scratch image copies in: the Mozilla CA
  certificate bundle (MPL-2.0), the IANA tz database (public domain), and the two account files
  built on `alpine-baselayout` (GPL-2.0-only) bases. A refuse-by-default override table
  hand-corrects the rows `go-licenses` gets wrong — `modernc.org/mathutil` (unclassifiable
  standard BSD-3-Clause text) and `modernc.org/libc` (misreported as MIT via a notices file; the
  module's own LICENSE is BSD-3-Clause) — and the generator refuses any Unknown or non-https
  license link rather than committing a dead or machine-local one. An inventory-only edit keeps
  the analysis lane running, so a hand edit cannot dodge the gate. No behaviour changes.
