### Internal

- **`scripts/archcheck` enforces the architecture contract against the tree.** Two invariants
  that were checked only as text — nothing under `internal/api` or `internal/apideps` imports
  persistence, and no schema is migrated at runtime — are now decided by parsing the module and,
  where the answer needs it, type-checking it: a `gorm.DB` reached through a wrapper is caught,
  and a same-named method that gorm did not declare is not. It runs in CI, refuses a commit
  through `.githooks/pre-commit`, and reports nothing about a tree it could not read rather than
  reporting it clean. Adds `golang.org/x/tools` as a direct dependency.
