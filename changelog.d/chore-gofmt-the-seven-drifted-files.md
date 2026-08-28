### Internal

- **Seven files are `gofmt`-clean again.** The release checklist declares a formatting gate before
  a tag, and the tree failed it: `gofmt -l` over the scoped package list named two `internal/api`
  regression tests, four `internal/services` files and `scripts/changelogd/main_test.go`. Six were
  struct- and comment-alignment drift; `webhook_reminder.go` also gains the blank comment lines
  gofmt inserts between the items of a list in a doc comment. No behaviour changes — `git diff -w`
  is empty except for those comment lines.
