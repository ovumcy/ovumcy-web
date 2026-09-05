none

Test-only fix: the `TestOnlyBootstrapBuildsRepositories` guard walked the
entire physical directory tree under the module root, so a nested checkout of
another repository (or another worktree of this one) sitting under it — with
its own `go.mod`, its own git marker, and any leftover file that happened to
contain the exact `db.NewRepositories(` text the guard looks for — made the
guard fail on an otherwise-unchanged product tree. The walk now stops at any
directory that is itself the root of a different module or repository
checkout (its own `go.mod` and/or its own git marker), plus dot-directories
and `node_modules`, and no longer relies on a hardcoded list of directory
names. The guard still catches an unwired `db.NewRepositories(` call anywhere
inside the tracked tree.
