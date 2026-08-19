none

Test-only fix: the theme-persistence scenario proved nothing about the save it
was named for. Every signal it waited on after pressing save was written by the
client before any network I/O, and the one that looked server-backed — the save
button falling back to disabled — only meant that some page had loaded. With the
interface endpoint persisting nothing and confirming nothing, the scenario still
ran green. It now waits on the save click's own PATCH, asserts that response,
and reads back the success notice the server renders. No product code involved.
