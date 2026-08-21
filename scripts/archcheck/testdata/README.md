# archcheck fixtures

Go sources kept as `.go.txt` data rather than as `.go` files, for two reasons that point the
same way.

The rule these fixtures exercise forbids a gorm `AutoMigrate` call in this module's Go source.
A fixture is a violation on purpose, so a fixture that WAS Go source would either have to be
exempted by name — an exemption the rule then carries forever — or would make the tree state
this command reports on depend on the command's own tests. Held as data, the tree stays clean by
construction and nothing needs excusing.

`testdata/` is also excluded from the scan itself (the go tool does not build it either), so a
fixture placed here is inert until a test copies it into a temporary module and runs the scan
against that.

One file per case, named for what it is about. The test that reads them is `../main_test.go`.
