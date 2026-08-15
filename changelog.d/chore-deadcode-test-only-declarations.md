none

Dead-code removal with no user-visible effect: `db.OpenSQLite` is gone and its
47 test call sites across `cmd/ovumcy`, `internal/api`, `internal/cli` and
`internal/db` now open through `db.OpenDatabase(db.Config{...})`, the same
configured entry point the server and the CLI subcommands use. The wrapper only
filled in a `Config` literal, so migrations and the SQLite PRAGMA/DSN form were
already identical on both paths and no test changes what it exercises.

`i18n.PluralCategories` stays where it is: it is the single source of truth for
which CLDR categories a language's locale files must carry, and it is read from
two packages' tests, so it cannot become a `_test.go` declaration. Its comment
now records that.
