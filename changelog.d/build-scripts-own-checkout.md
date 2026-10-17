none

Build tooling only: `scripts/build-js.mjs` and `scripts/build-css.mjs` now resolve their paths against
the checkout they live in, so a run started from another directory no longer reads and overwrites
that directory's tree. `npm run build` behaves as before.
