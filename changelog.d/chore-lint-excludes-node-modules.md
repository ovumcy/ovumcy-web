none

Lint configuration only, with no effect on the shipped application. `npm ci` installs at least one
package that ships Go sources of its own — `flatted`, whose Go port sits beside its JavaScript — and
a bare `golangci-lint run` walked into it and reported five findings (2 govet, 3 intrange) in a file
nothing in this repository calls, that is untracked, and that the next install overwrites. Anyone
running the linter without scoping it by hand read a verdict that was partly about someone else's
code, and the findings invited a fix that could not survive an install.

`linters.exclusions.paths` now names `node_modules`, so the plain command is the correct command.
CI was never affected and its step is unchanged: it names the module's package trees explicitly, in
a job that does not run `npm ci` at all.
