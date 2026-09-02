### Dependencies

- `@humanfs/node` 0.16.7 → 0.16.8, closing GHSA-p498-v437-472g (moderate): its recursive copy followed symlinked files and copied data from outside the source tree. The package is a dev-scope transitive dependency of `eslint` and is never built into the runtime image. `eslint`'s own range already admits the patched version, so the lockfile carries the fix and no manifest pin was added.
