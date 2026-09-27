### Internal

- **The release tag's ancestry check is now exercised against a real repository.** `verify-release-tag`
  refuses a tag whose commit `main` does not contain, and until now no test ran that step. The
  release-gate suite now extracts the step's own script and runs it inside a real clone of a
  fixture origin: tags on or behind `main`'s tip pass, lightweight or annotated, and a tag on a
  branch `main` never merged is refused — also when `main`'s history mentions that commit in a
  message, and when the checkout's `origin/main` was left pointing at it. The job's checkout is
  pinned to a full history, which the check cannot answer without.
