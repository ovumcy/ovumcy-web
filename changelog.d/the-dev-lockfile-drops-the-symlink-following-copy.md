### Dependencies

- `@humanfs/node` 0.16.7 → 0.16.8, closing GHSA-p498-v437-472g (moderate): its recursive copy followed symlinked files and copied data from outside the source tree. The package is a dev-scope transitive dependency of `eslint` and is never built into the runtime image. An `overrides` entry keeps the floor at `^0.16.8`, so no other dependent can reintroduce the vulnerable version under `eslint`'s wider `^0.16.6` range.
