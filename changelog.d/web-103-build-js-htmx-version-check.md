none

Dev tooling only: the JS build now takes the htmx banner version from the installed
package and refuses to build when `node_modules` disagrees with `package-lock.json`.
The shipped bundle is byte-identical; no user-facing effect.
